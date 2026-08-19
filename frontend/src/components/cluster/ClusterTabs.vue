<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'

import StateDot from '@/components/common/StateDot.vue'
import { useClusterStore } from '@/stores/clusters'

/**
 * One tab per open cluster.
 *
 * Each carries its own client, namespace, watches and port forwards on the Go
 * side, so switching tabs never risks aiming an action at the customer that
 * happened to be open a moment ago.
 */
const clusters = useClusterStore()
const router = useRouter()
const route = useRoute()

async function select(clusterId: string) {
  clusters.activeId = clusterId
  await router.push({ name: 'overview', params: { clusterId } })
}

async function close(clusterId: string) {
  clusters.close(clusterId)
  const next = clusters.activeId
  await router.push(next ? { name: 'overview', params: { clusterId: next } } : { name: 'clusters' })
}
</script>

<template>
  <nav
    v-if="clusters.openClusters.length"
    class="flex h-9 shrink-0 items-stretch gap-px overflow-x-auto border-b border-line bg-surface-1"
  >
    <button
      v-for="cluster in clusters.openClusters"
      :key="cluster.id"
      class="group flex items-center gap-2 border-r border-line px-3 text-xs"
      :class="
        String(route.params.clusterId) === cluster.id
          ? 'bg-surface-0 text-ink'
          : 'text-ink-muted hover:text-ink'
      "
      @click="select(cluster.id)"
    >
      <StateDot
        :state="clusters.sessions[cluster.id]?.state"
        :pulse="clusters.sessions[cluster.id]?.state === 'connecting'"
      />
      <span class="max-w-40 truncate">
        {{ cluster.customerName || cluster.customerId }} · {{ cluster.name }}
      </span>
      <span
        v-if="cluster.environmentKind === 'production'"
        class="rounded bg-warn/20 px-1 text-[9px] font-bold tracking-wider text-warn"
      >
        PROD
      </span>
      <span
        class="ml-1 text-ink-faint opacity-0 transition group-hover:opacity-100 hover:text-ink"
        role="button"
        aria-label="Close tab"
        @click.stop="close(cluster.id)"
      >
        ×
      </span>
    </button>
  </nav>
</template>
