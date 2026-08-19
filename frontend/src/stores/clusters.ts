import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { api, events, message, on } from '@/api'
import { ClusterState } from '@/types'
import type { AccessState, Cluster, ClusterView, KindInfo, Session } from '@/types'

/**
 * Cluster state for the UI.
 *
 * Only presentation data lives here: identifiers, names, connection state and
 * the namespace in view. Credentials, tokens and kubeconfig contents stay in
 * the Go process, where they can be held briefly and dropped.
 */
export const useClusterStore = defineStore('clusters', () => {
  const clusters = ref<Cluster[]>([])
  const sessions = ref<Record<string, Session>>({})
  const namespaces = ref<Record<string, string[]>>({})
  const catalogues = ref<Record<string, KindInfo[]>>({})
  const accessStates = ref<Record<string, AccessState>>({})

  const activeId = ref<string>('')

  /** openIds are the clusters with a tab, in the order they were opened. */
  const openIds = ref<string[]>([])

  const loading = ref(false)
  const error = ref('')

  const active = computed(() => clusters.value.find((c) => c.id === activeId.value))
  const activeSession = computed<Session | undefined>(() =>
    activeId.value ? sessions.value[activeId.value] : undefined,
  )
  const activeNamespace = computed(() => activeSession.value?.namespace ?? '')

  const openClusters = computed(() =>
    openIds.value
      .map((id) => clusters.value.find((c) => c.id === id))
      .filter((c): c is Cluster => Boolean(c)),
  )

  /** byCustomer groups the sidebar, because engineers think customer-first. */
  const byCustomer = computed(() => {
    const groups = new Map<string, Cluster[]>()
    for (const cluster of clusters.value) {
      const key = cluster.customerName || cluster.customerId || 'Ungrouped'
      groups.set(key, [...(groups.get(key) ?? []), cluster])
    }
    return [...groups.entries()].map(([customer, items]) => ({ customer, items }))
  })

  function absorb(views: ClusterView[]) {
    clusters.value = views.map((view) => view.cluster)
    for (const view of views) {
      sessions.value[view.cluster.id] = view.session
    }
  }

  async function load() {
    loading.value = true
    error.value = ''
    try {
      absorb(await api.listClusters())
    } catch (err) {
      error.value = message(err)
    } finally {
      loading.value = false
    }
  }

  async function connect(clusterId: string) {
    sessions.value[clusterId] = {
      ...(sessions.value[clusterId] ??
        { clusterId, state: ClusterState.ClusterDisconnected, namespace: '' }),
      state: ClusterState.ClusterConnecting,
    }
    try {
      sessions.value[clusterId] = await api.connectCluster(clusterId)
      await Promise.all([loadNamespaces(clusterId), loadCatalogue(clusterId)])
    } catch (err) {
      error.value = message(err)
    }
  }

  async function disconnect(clusterId: string) {
    sessions.value[clusterId] = await api.disconnectCluster(clusterId)
  }

  /** open puts a cluster in a tab and connects it if it is not already up. */
  async function open(clusterId: string) {
    if (!openIds.value.includes(clusterId)) {
      openIds.value = [...openIds.value, clusterId]
    }
    activeId.value = clusterId

    const session = sessions.value[clusterId]
    if (
      !session ||
      session.state === ClusterState.ClusterDisconnected ||
      session.state === ClusterState.ClusterFailed
    ) {
      await connect(clusterId)
    }
  }

  /** close leaves the cluster connected: a closed tab is not a disconnect. */
  function close(clusterId: string) {
    openIds.value = openIds.value.filter((id) => id !== clusterId)
    if (activeId.value === clusterId) {
      activeId.value = openIds.value[openIds.value.length - 1] ?? ''
    }
  }

  async function loadNamespaces(clusterId: string) {
    namespaces.value[clusterId] = await api.listNamespaces(clusterId)
  }

  async function loadCatalogue(clusterId: string) {
    catalogues.value[clusterId] = await api.resourceCatalogue(clusterId)
  }

  async function setNamespace(clusterId: string, namespace: string) {
    await api.setNamespace(clusterId, namespace)
    const session = sessions.value[clusterId]
    if (session) sessions.value[clusterId] = { ...session, namespace }
  }

  async function refreshAccess(profileId: string) {
    if (!profileId) return
    accessStates.value[profileId] = await api.accessStatus(profileId)
  }

  async function connectWithAccess(cluster: Cluster) {
    const profileId = cluster.access.profileId ?? ''
    await api.connectWithAccess(profileId, cluster.customerId)
  }

  /**
   * subscribe wires Go events into the store.
   *
   * Sessions are pushed rather than polled: a cluster can change state because
   * a VPN came up in another application, with nothing happening in this one.
   */
  function subscribe() {
    on(events.session, (session) => {
      sessions.value[session.clusterId] = session
      if (session.state === ClusterState.ClusterConnected) {
        void loadNamespaces(session.clusterId)
        void loadCatalogue(session.clusterId)
      }
    })
    on(events.accessChanged, (event) => {
      void refreshAccess(event.profileId)
    })
  }

  return {
    clusters,
    sessions,
    namespaces,
    catalogues,
    accessStates,
    activeId,
    openIds,
    loading,
    error,
    active,
    activeSession,
    activeNamespace,
    openClusters,
    byCustomer,
    load,
    connect,
    disconnect,
    open,
    close,
    setNamespace,
    refreshAccess,
    connectWithAccess,
    subscribe,
  }
})
