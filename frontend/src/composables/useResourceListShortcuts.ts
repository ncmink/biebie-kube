import { type Ref, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { isTypingTarget, matchChord, modKey } from '@/composables/shortcutKeys'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { QueryMode } from '@/types'
import type { ResourceRow } from '@/types'

export function useResourceListShortcuts(options: {
  clusterId: string
  kind: string
  selected: Ref<ResourceRow | null>
  deleting: Ref<ResourceRow | null>
  filterInput: Ref<HTMLInputElement | undefined>
  expressionInput: Ref<HTMLInputElement | undefined>
  onRefresh: () => void
}) {
  const route = useRoute()
  const router = useRouter()
  const clusters = useClusterStore()
  const resources = useResourceStore()
  const ui = useUIStore()

  const focusIndex = ref(-1)

  const active = () =>
    route.name === 'resources' &&
    String(route.params.clusterId) === options.clusterId &&
    String(route.params.kind) === options.kind &&
    clusters.sessions[options.clusterId]?.state === 'connected'

  function syncFocusFromSelection() {
    if (!options.selected.value) {
      focusIndex.value = -1
      return
    }
    const index = resources.rows.findIndex((row) => row.key === options.selected.value?.key)
    focusIndex.value = index
  }

  watch(() => options.selected.value?.key, syncFocusFromSelection)
  watch(
    () => resources.rows.length,
    () => {
      if (focusIndex.value >= resources.rows.length) {
        focusIndex.value = resources.rows.length ? resources.rows.length - 1 : -1
      }
    },
  )

  function moveFocus(delta: number) {
    const count = resources.rows.length
    if (!count) return
    if (focusIndex.value < 0) focusIndex.value = delta > 0 ? 0 : count - 1
    else focusIndex.value = (focusIndex.value + delta + count) % count
  }

  function focusedRow(): ResourceRow | null {
    if (focusIndex.value < 0) return null
    return resources.rows[focusIndex.value] ?? null
  }

  async function openLogsTerminal(row: ResourceRow) {
    await router.push({
      name: 'resource',
      params: {
        clusterId: options.clusterId,
        kind: options.kind,
        namespace: row.namespace || '_',
        name: row.name,
      },
      query: { tab: 'Logs' },
    })
  }

  function onKeydown(event: KeyboardEvent) {
    if (!active()) return
    if (ui.paletteOpen || ui.switcher || ui.shortcutsOpen) return

    if (matchChord(event, { key: 'Enter', mod: true })) {
      const row = options.selected.value ?? focusedRow()
      if (!row) return
      event.preventDefault()
      void openLogsTerminal(row)
      return
    }

    if (!modKey(event) && matchChord(event, { key: 'Enter', mod: false })) {
      if (isTypingTarget(event.target)) return
      const row = focusedRow()
      if (!row) return
      event.preventDefault()
      options.selected.value = row
      return
    }

    if (matchChord(event, { key: 'r', shift: true, mod: true })) {
      event.preventDefault()
      options.onRefresh()
      return
    }

    if (matchChord(event, { key: 'Backspace', mod: true })) {
      const row = options.selected.value ?? focusedRow()
      if (!row) return
      event.preventDefault()
      options.deleting.value = row
      return
    }

    if (modKey(event)) return
    if (isTypingTarget(event.target)) return

    if (event.key === '/') {
      event.preventDefault()
      const target =
        resources.queryMode === QueryMode.QueryModeExpression
          ? options.expressionInput.value
          : options.filterInput.value
      target?.focus()
      return
    }

    if (event.key === 'Escape') {
      if (options.selected.value) {
        event.preventDefault()
        options.selected.value = null
      }
      return
    }

    if (event.key === 'ArrowDown' || event.key === 'j') {
      event.preventDefault()
      moveFocus(1)
      return
    }
    if (event.key === 'ArrowUp' || event.key === 'k') {
      event.preventDefault()
      moveFocus(-1)
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown, true))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown, true))

  return { focusIndex }
}
