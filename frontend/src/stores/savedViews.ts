import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref } from 'vue'

import { api, message } from '@/api'
import type { SavedView, SavedViewInput, SavedViewResolution } from '@/types'

export const useSavedViewsStore = defineStore('savedViews', () => {
  const byCluster = ref<Record<string, SavedView[]>>({})
  const loading = ref<Record<string, boolean>>({})

  async function load(clusterId: string) {
    if (!clusterId || loading.value[clusterId]) return
    loading.value = { ...loading.value, [clusterId]: true }
    try {
      const views = await api.listSavedViews(clusterId)
      byCluster.value = { ...byCluster.value, [clusterId]: views }
    } finally {
      loading.value = { ...loading.value, [clusterId]: false }
    }
  }

  async function save(input: SavedViewInput): Promise<SavedView> {
    const view = await api.saveSavedView(input)
    const existing = byCluster.value[input.clusterId] ?? []
    const next = existing.some((entry) => entry.id === view.id)
      ? existing.map((entry) => (entry.id === view.id ? view : entry))
      : [view, ...existing]
    byCluster.value = { ...byCluster.value, [input.clusterId]: next }
    return view
  }

  async function remove(clusterId: string, viewId: string) {
    await api.deleteSavedView(clusterId, viewId)
    const existing = byCluster.value[clusterId] ?? []
    byCluster.value = {
      ...byCluster.value,
      [clusterId]: existing.filter((entry) => entry.id !== viewId),
    }
  }

  async function resolve(clusterId: string, viewId: string): Promise<SavedViewResolution> {
    return api.resolveSavedView(clusterId, viewId)
  }

  function forgetCluster(clusterId: string) {
    const next = { ...byCluster.value }
    delete next[clusterId]
    byCluster.value = next
  }

  return {
    byCluster,
    load,
    save,
    remove,
    resolve,
    forgetCluster,
    errorText: message,
  }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useSavedViewsStore, import.meta.hot))
}
