<script setup lang="ts">
import { computed } from 'vue'

import ConnectionDiagnosis from './ConnectionDiagnosis.vue'
import EnvironmentBadge from '@/components/common/EnvironmentBadge.vue'
import StateDot from '@/components/common/StateDot.vue'
import { message } from '@/api'
import { accessHealth, accessLabel, stateLabel } from '@/composables/format'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import { Health } from '@/types'
import type { Cluster } from '@/types'

const props = defineProps<{ cluster: Cluster }>()
const emit = defineEmits<{ open: []; menu: [event: MouseEvent] }>()

const clusters = useClusterStore()
const ui = useUIStore()

const session = computed(() => clusters.sessions[props.cluster.id])
const access = computed(() =>
  props.cluster.access.profileId ? clusters.accessStates[props.cluster.access.profileId] : undefined,
)

/** opening is true from the click until the network is up or the ask lapses. */
const opening = computed(() => clusters.accessOpening(props.cluster.access.profileId ?? ''))

/** launching is the narrower gap before Biebie Access has said anything back. */
const launching = computed(() =>
  Boolean(clusters.accessAsked[props.cluster.access.profileId ?? '']),
)

// A cluster that needs no customer network is neither healthy nor unhealthy in
// this respect, so it shows the neutral dot rather than a green one it has not
// earned.
const accessDot = computed(() => {
  if (!props.cluster.access.required) return Health.HealthUnknown
  if (opening.value) return Health.HealthProgress
  return accessHealth(access.value?.status.state)
})

const accessText = computed(() => {
  if (!props.cluster.access.required) return 'Not required'
  if (launching.value) return 'Opening Biebie Access…'
  const state = access.value
  if (!state) return 'Checking…'
  if (!state.installed) return 'Biebie Access is not running'
  return accessLabel(state.status.state)
})

/** The button says what it is waiting on rather than inviting another click. */
const accessButton = computed(() => {
  if (launching.value) return 'Opening Biebie Access…'
  if (opening.value) return 'Connecting…'
  return 'Connect with Biebie Access'
})

async function connectAccess() {
  try {
    await clusters.connectWithAccess(props.cluster)
    ui.say('Asked Biebie Access to connect this customer. Biebie Kube will retry on its own.')
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}
</script>

<template>
  <article
    class="relative overflow-hidden rounded-2xl border bg-surface-2 p-5"
    :class="cluster.environmentKind === 'production' ? 'border-warn/40' : 'border-line'"
    @contextmenu.prevent="emit('menu', $event)"
  >
    <div
      v-if="cluster.environmentKind === 'production'"
      class="production-band absolute inset-x-0 top-0 h-1"
      aria-hidden="true"
    />

    <header class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="truncate text-sm font-semibold text-ink">{{ cluster.name }}</h3>
        <p class="mt-0.5 truncate font-mono text-xs text-ink-faint">{{ cluster.server }}</p>
      </div>
      <EnvironmentBadge :kind="cluster.environmentKind" :label="cluster.environmentName" />
    </header>

    <dl class="mt-4 space-y-1 text-xs">
      <div
        class="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 transition-colors"
        :class="opening ? 'bg-info/10 ring-1 ring-inset ring-info/30' : ''"
      >
        <dt class="w-16 shrink-0 text-ink-faint">Access</dt>
        <dd class="flex min-w-0 items-center gap-1.5" :class="opening ? 'text-info' : 'text-ink-muted'">
          <StateDot :health="accessDot" :pulse="opening" />
          <span class="truncate">{{ accessText }}</span>
        </dd>
      </div>
      <div class="-mx-1.5 flex items-center gap-2 px-1.5 py-1">
        <dt class="w-16 shrink-0 text-ink-faint">Cluster</dt>
        <dd class="flex min-w-0 items-center gap-1.5 text-ink-muted">
          <StateDot :state="session?.state" :pulse="session?.state === 'connecting'" />
          <span class="truncate">{{ stateLabel(session?.state) }}</span>
          <span v-if="session?.serverVersion" class="truncate font-mono text-ink-faint">
            {{ session.serverVersion }}
          </span>
        </dd>
      </div>
    </dl>

    <ConnectionDiagnosis
      v-if="session?.diagnosis && session.state !== 'connected'"
      :diagnosis="session.diagnosis"
      class="mt-4"
    />

    <footer class="mt-5 flex flex-wrap gap-2">
      <button
        class="rounded-lg bg-brand px-3 py-1.5 text-xs font-semibold text-surface-1 hover:bg-brand-strong"
        @click="emit('open')"
      >
        {{ session?.state === 'connected' ? 'Open cluster' : 'Connect and open' }}
      </button>
      <button
        v-if="cluster.access.required && !access?.status.connected"
        class="rounded-lg border px-3 py-1.5 text-xs transition-colors"
        :class="
          opening
            ? 'border-info/40 bg-info/10 text-info'
            : 'border-line text-ink-muted hover:text-ink'
        "
        @click="connectAccess"
      >
        {{ accessButton }}
      </button>
      <button
        v-if="session?.state === 'connected'"
        class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="clusters.disconnect(cluster.id)"
      >
        Disconnect
      </button>
    </footer>
  </article>
</template>
