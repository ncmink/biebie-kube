<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import ContextTrail from './ContextTrail.vue'
import StateDot from './StateDot.vue'
import logo from '@/assets/images/logo-bb-kube-112.png'
import { stateLabel } from '@/composables/format'
import { useClusterStore } from '@/stores/clusters'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'

const clusters = useClusterStore()
const forwards = usePortForwardStore()
const ui = useUIStore()
const route = useRoute()

const clusterId = computed(() => String(route.params.clusterId ?? ''))
const cluster = computed(() => clusters.clusters.find((c) => c.id === clusterId.value))
const session = computed(() => (clusterId.value ? clusters.sessions[clusterId.value] : undefined))

const running = computed(() => forwards.forwards.filter((f) => f.state === 'running').length)
</script>

<template>
  <header
    class="drag-region flex h-12 shrink-0 items-center gap-4 border-b border-line bg-surface-1 pr-3 pl-20"
  >
    <RouterLink :to="{ name: 'clusters' }" class="no-drag flex shrink-0 items-center gap-2">
      <!-- The wordmark beside it already names this link, so the image is left
           unlabelled rather than having a screen reader read the name twice. -->
      <img :src="logo" alt="" class="size-7 shrink-0" />
      <span class="text-sm font-semibold tracking-tight">Biebie Kube</span>
    </RouterLink>

    <div v-if="cluster" class="no-drag flex min-w-0 flex-1 items-center gap-3">
      <span class="h-5 w-px bg-line" aria-hidden="true" />
      <ContextTrail :cluster="cluster" class="min-w-0" />
      <span class="flex shrink-0 items-center gap-1.5 text-xs text-ink-muted">
        <StateDot :state="session?.state" :pulse="session?.state === 'connecting'" />
        {{ stateLabel(session?.state) }}
      </span>
    </div>
    <div v-else class="flex-1" />

    <button
      class="no-drag rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
      title="Command palette"
      @click="ui.paletteOpen = true"
    >
      ⌘K
    </button>

    <RouterLink
      :to="{ name: 'forwards' }"
      class="no-drag flex items-center gap-1.5 rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
    >
      Port forwards
      <span
        v-if="running"
        class="rounded-full bg-ok/20 px-1.5 font-mono text-[10px] text-ok"
      >{{ running }}</span>
    </RouterLink>

    <RouterLink
      :to="{ name: 'settings' }"
      class="no-drag rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
    >
      Settings
    </RouterLink>
  </header>
</template>
