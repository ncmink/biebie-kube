<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import QuickSwitcher from './QuickSwitcher.vue'
import type { SwitcherOption } from './QuickSwitcher.vue'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()
const route = useRoute()

const clusterId = computed(() => String(route.params.clusterId ?? clusters.activeId ?? ''))
const connected = computed(() => clusters.sessions[clusterId.value]?.state === 'connected')

const clusterOptions = computed<SwitcherOption[]>(() => {
  const tabs = clusters.openClusters.map((cluster) => ({
    id: `open:${cluster.id}`,
    label: `${cluster.customerName || cluster.customerId} · ${cluster.name}`,
    hint: 'Open tab',
  }))
  return [...tabs, { id: 'more', label: 'Open another cluster…', hint: 'Home' }]
})

const namespaceOptions = computed<SwitcherOption[]>(() => {
  const list = clusters.namespaces[clusterId.value] ?? []
  return [
    { id: 'ns:', label: 'All namespaces', hint: 'Namespace' },
    ...list.map((namespace) => ({
      id: `ns:${namespace}`,
      label: namespace,
      hint: 'Namespace',
    })),
  ]
})

const kindOptions = computed<SwitcherOption[]>(() => {
  const catalogue = clusters.catalogues[clusterId.value] ?? []
  return catalogue.map((entry) => ({
    id: entry.kind,
    label: entry.title,
    hint: entry.category,
  }))
})

function close() {
  ui.switcher = null
}

async function onCluster(id: string) {
  close()
  if (id === 'more') {
    await router.push({ name: 'clusters' })
    return
  }
  const clusterId = id.replace(/^open:/, '')
  clusters.activeId = clusterId
  await clusters.open(clusterId)
  await router.push({ name: 'overview', params: { clusterId } })
}

async function onNamespace(id: string) {
  close()
  if (!clusterId.value) return
  const value = id.startsWith('ns:') ? id.slice(3) : id
  await clusters.setNamespace(clusterId.value, value)
}

async function onKind(kind: string) {
  close()
  if (!clusterId.value) return
  await router.push({ name: 'resources', params: { clusterId: clusterId.value, kind } })
}
</script>

<template>
  <QuickSwitcher
    :open="ui.switcher === 'cluster'"
    title="Switch cluster"
    placeholder="Filter clusters…"
    :options="clusterOptions"
    @close="close"
    @select="onCluster"
  />
  <QuickSwitcher
    v-if="connected"
    :open="ui.switcher === 'namespace'"
    title="Switch namespace"
    placeholder="Filter namespaces…"
    :options="namespaceOptions"
    @close="close"
    @select="onNamespace"
  />
  <QuickSwitcher
    v-if="connected"
    :open="ui.switcher === 'kind'"
    title="Go to resource kind"
    placeholder="Filter kinds…"
    :options="kindOptions"
    @close="close"
    @select="onKind"
  />
</template>
