<script setup lang="ts">
import { computed, onMounted } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { forwardHealth } from '@/composables/format'
import { message, openInBrowser } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'

const clusters = useClusterStore()
const sessions = usePortForwardStore()
const ui = useUIStore()

const grouped = computed(() => {
  const byCluster = new Map<string, typeof sessions.forwards>()
  for (const forward of sessions.forwards) {
    byCluster.set(forward.clusterId, [...(byCluster.get(forward.clusterId) ?? []), forward])
  }
  return [...byCluster.entries()]
})

function label(clusterId: string) {
  const cluster = clusters.clusters.find((entry) => entry.id === clusterId)
  if (!cluster) return clusterId
  return `${cluster.customerName} · ${cluster.name}`
}

async function stop(id: string) {
  try {
    await sessions.stop(id)
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}

onMounted(() => void sessions.load())
</script>

<template>
  <div class="h-full overflow-y-auto px-6 py-6">
    <h1 class="text-sm font-semibold uppercase tracking-widest text-ink-faint">Port forwards</h1>

    <p v-if="!sessions.forwards.length" class="mt-6 text-sm text-ink-muted">
      Nothing is being forwarded. Start one from a pod's detail view.
    </p>

    <section v-for="[clusterId, forwards] in grouped" :key="clusterId" class="mt-5">
      <h2 class="text-xs font-semibold text-ink">{{ label(clusterId) }}</h2>
      <ul class="mt-2 space-y-2">
        <li
          v-for="forward in forwards"
          :key="forward.id"
          class="flex items-center gap-3 rounded-xl border border-line bg-surface-2 px-4 py-3"
        >
          <StateDot :health="forwardHealth(forward.state)" :pulse="forward.state === 'starting'" />
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm text-ink">{{ forward.resourceName }}</p>
            <p class="truncate font-mono text-xs text-ink-muted">
              localhost:{{ forward.localPort }} → {{ forward.remotePort }} ·
              {{ forward.namespace }}
            </p>
            <p v-if="forward.error" class="mt-0.5 text-xs text-bad">{{ forward.error }}</p>
          </div>
          <button
            v-if="forward.state === 'running'"
            class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
            @click="openInBrowser(`http://localhost:${forward.localPort}`)"
          >
            Open browser
          </button>
          <button
            class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
            @click="stop(forward.id)"
          >
            Stop
          </button>
        </li>
      </ul>
    </section>
  </div>
</template>
