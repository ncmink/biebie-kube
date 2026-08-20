import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { api, events, message, on } from '@/api'
import { ClusterState } from '@/types'
import type {
  AccessState,
  Cluster,
  ClusterView,
  CustomerGroup,
  KindInfo,
  Session,
} from '@/types'

/** Whether hidden customer groups are currently revealed. A view preference. */
const showHiddenKey = 'biebie-kube.show-hidden-groups'

/**
 * Cluster state for the UI.
 *
 * Only presentation data lives here: identifiers, names, connection state and
 * the namespace in view. Credentials, tokens and kubeconfig contents stay in
 * the Go process, where they can be held briefly and dropped.
 */
export const useClusterStore = defineStore('clusters', () => {
  const clusters = ref<Cluster[]>([])
  const groups = ref<CustomerGroup[]>([])
  const showHidden = ref(localStorage.getItem(showHiddenKey) === '1')
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

  /**
   * byCustomer is the cluster list as Go grouped it, because engineers think
   * customer-first and the grouping has to agree with what the backend hides.
   * A group whose clusters are all gone is dropped rather than shown empty.
   */
  const byCustomer = computed(() =>
    groups.value
      .filter((group) => showHidden.value || !group.hidden)
      .map((group) => ({
        key: group.key,
        customer: group.label,
        hidden: group.hidden,
        // The clusters that name no customer are not a customer, so that
        // section is shown without the control that would put it away.
        hideable: group.key !== '',
        items: (group.clusterIds ?? [])
          .map((id) => clusters.value.find((cluster) => cluster.id === id))
          .filter((cluster): cluster is Cluster => Boolean(cluster)),
      }))
      .filter((group) => group.items.length > 0),
  )

  /** How many clusters sit in a hidden group, whether revealed or not. */
  const hiddenCount = computed(() =>
    groups.value
      .filter((group) => group.hidden)
      .reduce((total, group) => total + (group.clusterIds ?? []).length, 0),
  )

  const visibleCount = computed(() =>
    byCustomer.value.reduce((total, group) => total + group.items.length, 0),
  )

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
      const [views, grouping] = await Promise.all([
        api.listClusters(),
        api.listCustomerGroups(),
      ])
      absorb(views)
      groups.value = grouping
    } catch (err) {
      error.value = message(err)
    } finally {
      loading.value = false
    }
  }

  function setShowHidden(value: boolean) {
    showHidden.value = value
    localStorage.setItem(showHiddenKey, value ? '1' : '0')
  }

  function toggleShowHidden() {
    setShowHidden(!showHidden.value)
  }

  /** Hides or reveals one section of the list. Nothing is deleted or dropped. */
  async function setGroupHidden(key: string, hidden: boolean) {
    groups.value = await api.setCustomerGroupHidden(key, hidden)
  }

  /**
   * Puts one cluster in the archive or takes it back out.
   *
   * The archive is the section that stays hidden unless asked for, so this is
   * how a cluster goes out of the way without being deleted. Allowed while it
   * is connected: the session and its tab are untouched.
   */
  async function setArchived(clusterId: string, archived: boolean) {
    await api.setClusterArchived(clusterId, archived)
    await load()
  }

  /** Forgets a cluster. Biebie's record only — the kubeconfig is left alone. */
  async function remove(clusterId: string) {
    await api.deleteCluster(clusterId)
    close(clusterId)
    delete sessions.value[clusterId]
    await load()
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
    groups,
    showHidden,
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
    hiddenCount,
    visibleCount,
    load,
    setShowHidden,
    toggleShowHidden,
    setGroupHidden,
    setArchived,
    remove,
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

// A setup store is a closure, so a hot reload replaces the module while the
// running instance keeps the actions it was built with — a component calling one
// added a moment ago would find it undefined. Handing the update to Pinia
// rebuilds the instance instead, which is what makes editing a store during
// `wails3 dev` behave like editing a component.
if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useClusterStore, import.meta.hot))
}
