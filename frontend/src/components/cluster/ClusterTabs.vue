<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import StateDot from '@/components/common/StateDot.vue'
import { message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import { AccessMode } from '@/types'

/**
 * One tab per open cluster.
 *
 * Each carries its own client, namespace, watches and port forwards on the Go
 * side, so switching tabs never risks aiming an action at the customer that
 * happened to be open a moment ago.
 */
const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()
const route = useRoute()

const activeClusterId = computed(() => String(route.params.clusterId ?? clusters.activeId ?? ''))
const connected = computed(
  () => clusters.sessions[activeClusterId.value]?.state === 'connected',
)
const policy = computed(() =>
  activeClusterId.value ? clusters.policies[activeClusterId.value] : undefined,
)
const sessionReadOnly = computed(() => Boolean(policy.value?.sessionReadOnly))
const toggling = ref(false)

const canToggleSession = computed(
  () =>
    connected.value &&
    policy.value?.persistedMode !== AccessMode.AccessModeReadOnly &&
    !toggling.value,
)

watch(
  activeClusterId,
  (id) => {
    if (id) void clusters.loadPolicy(id)
  },
  { immediate: true },
)

async function select(clusterId: string) {
  clusters.activeId = clusterId
  await router.push({ name: 'overview', params: { clusterId } })
}

async function close(clusterId: string) {
  clusters.close(clusterId)
  const next = clusters.activeId
  await router.push(next ? { name: 'overview', params: { clusterId: next } } : { name: 'clusters' })
}

async function toggleSession() {
  if (!activeClusterId.value || !policy.value || !canToggleSession.value) return
  const next = !policy.value.sessionReadOnly
  toggling.value = true
  try {
    await clusters.setSessionReadOnly(activeClusterId.value, next)
    ui.say(
      next
        ? 'This session is read-only until you disconnect.'
        : 'This session is read-write again.',
    )
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    toggling.value = false
  }
}
</script>

<template>
  <nav
    v-if="clusters.openClusters.length"
    class="flex h-9 shrink-0 items-stretch border-b border-line bg-surface-1"
  >
    <div class="flex min-w-0 flex-1 items-stretch gap-px overflow-x-auto">
      <button
        v-for="cluster in clusters.openClusters"
        :key="cluster.id"
        class="group flex shrink-0 items-center gap-2 border-r border-line px-3 text-xs"
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
          v-if="clusters.policies[cluster.id]?.effectiveMode === AccessMode.AccessModeReadOnly"
          class="rounded bg-warn/20 px-1 text-[9px] font-bold tracking-wider text-warn"
        >
          RO
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
    </div>

    <div
      v-if="canToggleSession"
      class="flex shrink-0 items-center border-l border-line px-2"
    >
      <button
        type="button"
        class="inline-flex items-center gap-1.5 rounded-lg border border-line px-2 py-1 text-[11px] text-ink-muted hover:text-ink"
        :title="sessionReadOnly ? 'Allow writes this session' : 'Make this session read-only'"
        :disabled="toggling"
        @click="toggleSession"
      >
        <svg viewBox="0 0 16 16" class="size-3.5 shrink-0" fill="none" aria-hidden="true">
          <path
            v-if="sessionReadOnly"
            d="M5.5 7V5a2.5 2.5 0 015 0v2M4 7h8v6.5H4V7z"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linejoin="round"
          />
          <path
            v-else
            d="M5.5 7V5a2.5 2.5 0 015 0v2M4 7h8v6.5H4V7zM8 10v1.5"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
          />
        </svg>
        <span class="hidden sm:inline">{{ sessionReadOnly ? 'Allow writes' : 'Read-only' }}</span>
      </button>
    </div>
  </nav>
</template>
