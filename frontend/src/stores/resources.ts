import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { api, events, message, on } from '@/api'
import type { Column, ListQuery, ResourcePage, ResourceRow, RowsChanged } from '@/types'

/** The window the table asks for, and grows by as the user scrolls. */
const pageSize = 500

/** How long a keystroke waits before the cluster is asked again. */
const filterDelay = 150

/** The column the table starts sorted by, matching Go's default order. */
const defaultSortKey = 'createdAt'

type View = { clusterId: string; kind: string; namespace: string }

/**
 * The resource table currently in view.
 *
 * Go holds the whole set and this holds the window of it that is on screen,
 * which is why the filter, the order and the counts all come from there: a
 * filter applied here could only ever search the rows that happened to arrive,
 * and would report a resource that exists as missing.
 *
 * Updates arrive as patches rather than tables. A rollout in a large namespace
 * changes a handful of rows, and replacing the whole list three times a second
 * is what made a big cluster feel broken.
 */
export const useResourceStore = defineStore('resources', () => {
  const rows = ref<ResourceRow[]>([])
  const columns = ref<Column[]>([])
  const namespaced = ref(false)

  const total = ref(0)
  const matched = ref(0)

  const loading = ref(false)
  const appending = ref(false)
  /** syncing is set while the counts are still a floor: the watch is warming. */
  const syncing = ref(false)
  const error = ref('')

  const filter = ref('')
  // Newest first is the default because an engineer opening a list is almost
  // always looking for what just changed.
  const sortKey = ref(defaultSortKey)
  const sortDesc = ref(true)

  const current = ref<View | null>(null)

  // complete means the window holds everything the filter matched. While the
  // watch is still warming, matched is a floor, so this goes back to false when
  // the real count arrives and there is more to fetch after all.
  const complete = computed(() => rows.value.length >= matched.value)

  // The row index is deliberately outside Vue's reactivity: it is bookkeeping
  // for applying patches, and making every key observable would cost a
  // dependency notification per row for no reader.
  let byKey = new Map<string, ResourceRow>()
  let order: string[] = []
  let positions = new Map<string, number>()

  // token identifies the table being built. A patch names the query it was
  // computed from, so one still in flight when the filter changes is dropped
  // instead of being applied to a table it does not describe.
  let sequence = 0
  let token = ''
  let filterTimer: ReturnType<typeof setTimeout> | undefined

  function query(namespace: string, offset: number): ListQuery {
    return {
      namespace,
      filter: filter.value.trim(),
      sortKey: sortKey.value,
      sortDesc: sortDesc.value,
      offset,
      limit: pageSize,
      token,
    }
  }

  function isCurrent(view: View): boolean {
    const now = current.value
    return (
      now?.clusterId === view.clusterId &&
      now?.kind === view.kind &&
      now?.namespace === view.namespace
    )
  }

  function setOrder(keys: string[]) {
    order = keys
    positions = new Map()

    const next: ResourceRow[] = []
    for (const key of keys) {
      const row = byKey.get(key)
      if (!row) continue
      positions.set(key, next.length)
      next.push(row)
    }
    rows.value = next
  }

  function accept(page: ResourcePage) {
    byKey = new Map()
    const keys: string[] = []
    for (const row of page.rows ?? []) {
      byKey.set(row.key, row)
      keys.push(row.key)
    }
    columns.value = page.columns ?? []
    namespaced.value = page.namespaced
    total.value = page.total
    matched.value = page.matched
    syncing.value = page.loading ?? false
    setOrder(keys)
  }

  function append(page: ResourcePage) {
    const keys = order.slice()
    for (const row of page.rows ?? []) {
      if (byKey.has(row.key)) continue
      byKey.set(row.key, row)
      keys.push(row.key)
    }
    total.value = page.total
    matched.value = page.matched
    syncing.value = page.loading ?? false
    setOrder(keys)
  }

  /**
   * load reads the first window of a table.
   *
   * quiet keeps the rows on screen while the new ones are fetched, which is
   * what a changed filter or sort wants: a blank table between keystrokes reads
   * as "nothing found" for as long as the round trip takes.
   */
  async function load(clusterId: string, kind: string, namespace: string, quiet = false) {
    const view: View = { clusterId, kind, namespace }
    current.value = view
    token = String(++sequence)

    if (!quiet) {
      rows.value = []
      byKey = new Map()
      order = []
      positions = new Map()
      total.value = 0
      matched.value = 0
      loading.value = true
    }
    error.value = ''

    try {
      const page = await api.listResources(clusterId, kind, query(namespace, 0))
      // A slow response for a table the user has already navigated away from
      // must not overwrite the one now on screen.
      if (!isCurrent(view)) return
      accept(page)
    } catch (err) {
      if (isCurrent(view)) error.value = message(err)
    } finally {
      loading.value = false
    }
  }

  /** more extends the window, as the user scrolls towards the end of it. */
  async function more() {
    const view = current.value
    if (!view || loading.value || appending.value || complete.value) return

    appending.value = true
    try {
      const page = await api.listResources(
        view.clusterId,
        view.kind,
        query(view.namespace, rows.value.length),
      )
      if (!isCurrent(view)) return
      append(page)
    } catch (err) {
      if (isCurrent(view)) error.value = message(err)
    } finally {
      appending.value = false
    }
  }

  /** refine re-reads the table after the filter or the order changed. */
  function refine() {
    const view = current.value
    if (!view) return
    void load(view.clusterId, view.kind, view.namespace, true)
  }

  function setFilter(value: string) {
    filter.value = value
    clearTimeout(filterTimer)
    filterTimer = setTimeout(refine, filterDelay)
  }

  /**
   * sortBy toggles a column.
   *
   * Clicking the sorted column reverses it; clicking another starts it
   * descending, because every column an engineer reaches for — restarts, age,
   * status — is one where the interesting end is the top.
   */
  function sortBy(key: string) {
    if (sortKey.value === key) {
      sortDesc.value = !sortDesc.value
    } else {
      sortKey.value = key
      sortDesc.value = true
    }
    refine()
  }

  function reset() {
    clearTimeout(filterTimer)
    current.value = null
    rows.value = []
    columns.value = []
    byKey = new Map()
    order = []
    positions = new Map()
    total.value = 0
    matched.value = 0
    filter.value = ''
    sortKey.value = defaultSortKey
    sortDesc.value = true
    syncing.value = false
    error.value = ''
  }

  function patch(event: RowsChanged) {
    const view = current.value
    if (!view || event.token !== token) return
    if (event.clusterId !== view.clusterId || event.kind !== view.kind) return
    if (event.namespace !== view.namespace) return

    for (const row of event.upserts ?? []) byKey.set(row.key, row)
    for (const key of event.removed ?? []) byKey.delete(key)

    if (event.order) {
      setOrder(event.order)
    } else {
      // The order held, so the rows that changed are replaced where they sit
      // and the table does not re-render around them.
      for (const row of event.upserts ?? []) {
        const at = positions.get(row.key)
        if (at !== undefined) rows.value[at] = row
      }
    }

    total.value = event.total
    matched.value = event.matched
    syncing.value = event.loading
  }

  /** subscribe applies the patches Go sends for the table in view. */
  function subscribe() {
    on(events.rows, patch)
  }

  return {
    rows,
    columns,
    namespaced,
    total,
    matched,
    loading,
    appending,
    syncing,
    error,
    filter,
    sortKey,
    sortDesc,
    current,
    complete,
    load,
    more,
    setFilter,
    sortBy,
    reset,
    subscribe,
  }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useResourceStore, import.meta.hot))
}
