import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref } from 'vue'

import { api, events, message, on } from '@/api'
import type { ResourcePage } from '@/types'

/**
 * The resource table currently in view.
 *
 * One page is held at a time. Caching every kind for every namespace would
 * hold a large cluster's object graph in the renderer, which is exactly what
 * the informer architecture exists to avoid.
 */
export const useResourceStore = defineStore('resources', () => {
  const page = ref<ResourcePage | null>(null)
  const loading = ref(false)
  const error = ref('')
  const filter = ref('')

  const current = ref<{ clusterId: string; kind: string; namespace: string } | null>(null)

  async function load(clusterId: string, kind: string, namespace: string, quiet = false) {
    current.value = { clusterId, kind, namespace }
    if (!quiet) {
      loading.value = true
      page.value = null
    }
    error.value = ''
    try {
      const result = await api.listResources(clusterId, kind, namespace)
      // A slow response for a table the user has already navigated away from
      // must not overwrite the one now on screen.
      if (
        current.value?.clusterId === clusterId &&
        current.value?.kind === kind &&
        current.value?.namespace === namespace
      ) {
        page.value = result
      }
      void api.watchResources(clusterId, kind, namespace)
    } catch (err) {
      error.value = message(err)
    } finally {
      loading.value = false
    }
  }

  function reset() {
    page.value = null
    current.value = null
    filter.value = ''
    error.value = ''
  }

  /**
   * subscribe reloads the visible table when the cluster says it changed.
   *
   * The Go side has already debounced these, so a rollout produces a handful
   * of refreshes rather than one per pod event.
   */
  function subscribe() {
    on(events.resources, (event) => {
      const view = current.value
      if (!view || view.clusterId !== event.clusterId) return

      const matchesNamespace = event.namespace === '' || event.namespace === view.namespace
      if (!matchesNamespace) return

      void load(view.clusterId, view.kind, view.namespace, true)
    })
  }

  return { page, loading, error, filter, current, load, reset, subscribe }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useResourceStore, import.meta.hot))
}
