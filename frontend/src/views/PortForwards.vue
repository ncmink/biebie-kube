<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'

import EnvironmentBadge from '@/components/common/EnvironmentBadge.vue'
import StateDot from '@/components/common/StateDot.vue'
import { forwardHealth } from '@/composables/format'
import { message, openInBrowser } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind } from '@/types'
import type { Cluster, PortForwardSession } from '@/types'

const clusters = useClusterStore()
const sessions = usePortForwardStore()
const ui = useUIStore()
const router = useRouter()

/**
 * One section per cluster, because a local port means nothing on its own.
 *
 * Two customers both forwarding a service called argocd-server produce two
 * rows that differ only by port number, and the whole risk of this page is
 * opening the wrong one. The cluster is therefore the heading rather than a
 * column, and it carries the same identity a cluster wears everywhere else:
 * customer, name, connection state, and the production word.
 */
type Group = {
  clusterId: string
  cluster?: Cluster
  customer: string
  name: string
  environmentKind: EnvironmentKind
  environmentName: string
  forwards: PortForwardSession[]
}

const grouped = computed<Group[]>(() => {
  const byCluster = new Map<string, PortForwardSession[]>()
  for (const forward of sessions.forwards) {
    byCluster.set(forward.clusterId, [...(byCluster.get(forward.clusterId) ?? []), forward])
  }

  const groups = [...byCluster.entries()].map(([clusterId, forwards]) => {
    const cluster = clusters.clusters.find((entry) => entry.id === clusterId)
    return {
      clusterId,
      cluster,
      customer: cluster?.customerName || cluster?.customerId || '',
      // A cluster removed from Biebie Kube while its forward was running has
      // no record left to read, and its identifier is a UUID nobody
      // recognises. Go denormalised the name onto the session for exactly
      // this moment.
      name: cluster?.name || forwards[0]?.clusterName || clusterId,
      environmentKind: (cluster?.environmentKind as EnvironmentKind) ?? EnvironmentKind.EnvironmentUnknown,
      environmentName: cluster?.environmentName ?? '',
      forwards,
    }
  })

  // The backend orders forwards by cluster identifier, which is a UUID: the
  // sections would sit in an order nobody chose and reshuffle whenever a
  // cluster is added. Sorting by the words on screen keeps a section where the
  // engineer last saw it.
  return groups.sort(
    (a, b) => a.customer.localeCompare(b.customer) || a.name.localeCompare(b.name),
  )
})

const running = computed(
  () => sessions.forwards.filter((forward) => forward.state === 'running').length,
)

/**
 * Where the back arrow goes.
 *
 * Port forwards are a page of their own because a forward outlives the tab
 * that started it, which also means this is the one screen inside a cluster's
 * workflow with no cluster sidebar to click back through. The cluster last in
 * view is where the engineer came from; with no tab open at all there is
 * nothing to return to but the list.
 *
 * The arrow is only the way back. Reaching a cluster that is not the one they
 * arrived from is the section headings' job, because with forwards across
 * three customers there is no single cluster this page belongs to.
 */
const origin = computed(() => clusters.active)

const backLabel = computed(() =>
  origin.value
    ? `Back to ${origin.value.customerName || origin.value.customerId} · ${origin.value.name}`
    : 'Back to clusters',
)

function back() {
  void router.push(
    origin.value
      ? { name: 'overview', params: { clusterId: origin.value.id } }
      : { name: 'clusters' },
  )
}

function openCluster(group: Group) {
  if (!group.cluster) return
  void router.push({ name: 'overview', params: { clusterId: group.clusterId } })
}

async function stop(id: string) {
  try {
    await sessions.stop(id)
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}

/**
 * Stopping a whole cluster's forwards is one report, not one per tunnel.
 *
 * Closing eight forwards must not raise eight notices, and a batch where one
 * refuses is neither a success nor a failure — so both halves are counted and
 * said once.
 */
async function stopGroup(group: Group) {
  const total = group.forwards.length
  const failed = await sessions.stopMany(group.forwards.map((forward) => forward.id))
  const closed = total - failed

  if (failed) {
    ui.say(`Stopped ${closed} of ${total} forwards for ${group.name}.`, 'bad')
    return
  }
  ui.say(`Stopped ${closed} forward${closed === 1 ? '' : 's'} for ${group.name}.`)
}

onMounted(() => void sessions.load())
</script>

<template>
  <div class="h-full overflow-y-auto px-6 py-6">
    <div class="flex items-center gap-3">
      <button
        class="-ml-1.5 shrink-0 rounded-lg p-1.5 text-ink-muted transition-colors hover:bg-surface-2 hover:text-ink"
        :title="backLabel"
        :aria-label="backLabel"
        @click="back"
      >
        <svg viewBox="0 0 20 20" class="size-5" fill="none" aria-hidden="true">
          <path
            d="M16.5 10H4M9 4.5L3.5 10L9 15.5"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <h1 class="text-sm font-semibold uppercase tracking-widest text-ink-faint">Port forwards</h1>
      <span v-if="sessions.forwards.length" class="font-mono text-xs text-ink-faint">
        {{ running }} running · {{ grouped.length }}
        cluster{{ grouped.length === 1 ? '' : 's' }}
      </span>
    </div>

    <p v-if="!sessions.forwards.length" class="mt-6 text-sm text-ink-muted">
      Nothing is being forwarded. Start one from a pod's detail view.
    </p>

    <section v-for="group in grouped" :key="group.clusterId" class="mt-5">
      <header class="flex items-center gap-2">
        <button
          class="group flex min-w-0 items-center gap-2 rounded-lg px-1.5 py-1 text-left"
          :class="group.cluster ? 'hover:bg-surface-2' : 'cursor-default'"
          :disabled="!group.cluster"
          @click="openCluster(group)"
        >
          <StateDot
            :state="clusters.sessions[group.clusterId]?.state"
            :pulse="clusters.sessions[group.clusterId]?.state === 'connecting'"
          />
          <h2 class="min-w-0 truncate text-xs font-semibold text-ink">
            <span v-if="group.customer">{{ group.customer }} · </span>{{ group.name }}
          </h2>
          <EnvironmentBadge :kind="group.environmentKind" :label="group.environmentName" />
          <span
            v-if="group.cluster"
            class="text-xs text-ink-faint opacity-0 transition group-hover:opacity-100"
            aria-hidden="true"
          >
            →
          </span>
        </button>

        <span class="shrink-0 font-mono text-[11px] text-ink-faint">
          {{ group.forwards.length }}
        </span>

        <button
          v-if="group.forwards.length > 1"
          class="ml-auto shrink-0 rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
          @click="stopGroup(group)"
        >
          Stop all
        </button>
      </header>

      <!-- A forward whose cluster was removed cannot be navigated to, and the
           row is the only place left that says which customer it reaches. -->
      <p v-if="!group.cluster" class="mt-1 px-1.5 text-[11px] text-ink-faint">
        This cluster is no longer configured in Biebie Kube. The forward keeps running until it is
        stopped.
      </p>

      <ul class="mt-2 space-y-2">
        <li
          v-for="forward in group.forwards"
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
