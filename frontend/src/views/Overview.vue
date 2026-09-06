<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { api, message, openInBrowser } from '@/api'
import { age, bytes, millicores } from '@/composables/format'
import { Health } from '@/types'
import type { ClusterOverview, Forward } from '@/types'
import { useClusterStore } from '@/stores/clusters'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()

const overview = ref<ClusterOverview | null>(null)
const loading = ref(true)
const error = ref('')

/**
 * The API server's stand-in address, when Biebie Access is lending one.
 *
 * A loopback address on a cluster page reads as a mistake unless it says why it
 * is there, and the engineer needs to know which endpoint their own kubectl
 * would have to use to match what this window is showing.
 */
const apiForward = computed(() => clusters.sessions[props.clusterId]?.apiForward ?? null)

/**
 * The other ports the same connection lends. They are not Kubernetes — a
 * NodePort service, an admin console — but this is the page the engineer is on
 * when they want one, and a link is quicker than reading the tunnel back.
 */
const extraForwards = computed(() => clusters.sessions[props.clusterId]?.forwards ?? [])

/** The machine those ports are borrowed from, for naming the far side. */
const gateway = computed(() => clusters.sessions[props.clusterId]?.gateway ?? '')

/**
 * The cluster's own address — what the loopback port stands in for.
 *
 * The forward itself records the address as the SSH server resolves it, which
 * is usually its own `localhost` and says nothing on a page about a cluster
 * somewhere else. The kubeconfig's address is the one that means something here.
 */
const apiAddress = computed(
  () => clusters.clusters.find((c) => c.id === props.clusterId)?.server ?? '',
)

/** Where a lent port actually goes, said in full so neither end is guessed at. */
function remoteOf(forward: Forward): string {
  const host = forward.remoteHost.trim().toLowerCase()
  const onGateway = host === '' || host === 'localhost' || host === '127.0.0.1' || host === '::1'
  if (onGateway) {
    return gateway.value
      ? `Port ${forward.remotePort} on ${gateway.value}`
      : `Port ${forward.remotePort} on the SSH server`
  }
  return `${forward.remoteHost}:${forward.remotePort} in the customer network`
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    overview.value = await api.overview(props.clusterId)
  } catch (err) {
    error.value = message(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.clusterId, load)
</script>

<template>
  <div class="h-full overflow-y-auto px-6 py-6">
    <div class="flex items-center justify-between">
      <h1 class="text-sm font-semibold uppercase tracking-widest text-ink-faint">Cluster</h1>
      <button
        class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
        @click="load"
      >
        Refresh
      </button>
    </div>

    <p v-if="error" class="mt-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
      {{ error }}
    </p>

    <p v-else-if="loading" class="mt-6 text-sm text-ink-muted">Reading cluster state…</p>

    <template v-else-if="overview">
      <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <p class="text-xs text-ink-faint">Nodes</p>
          <p class="mt-1 font-mono text-2xl text-ink">
            {{ overview.nodes.ready }}<span class="text-ink-faint">/{{ overview.nodes.total }}</span>
          </p>
          <p class="mt-1 truncate text-xs text-ink-muted">{{ overview.platform || '—' }}</p>
        </div>
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <p class="text-xs text-ink-faint">Pods</p>
          <p class="mt-1 font-mono text-2xl text-ink">
            {{ overview.pods.ready }}<span class="text-ink-faint">/{{ overview.pods.total }}</span>
          </p>
          <p class="mt-1 text-xs text-ink-muted">{{ overview.namespaces }} namespaces</p>
        </div>
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <p class="text-xs text-ink-faint">Workloads</p>
          <p class="mt-1 font-mono text-2xl text-ink">
            {{ overview.deployments + overview.statefulSets + overview.daemonSets }}
          </p>
          <p class="mt-1 text-xs text-ink-muted">
            {{ overview.deployments }} deploy · {{ overview.statefulSets }} sts ·
            {{ overview.daemonSets }} ds
          </p>
        </div>
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <p class="text-xs text-ink-faint">Kubernetes</p>
          <p class="mt-1 truncate font-mono text-lg text-ink">{{ overview.serverVersion || '—' }}</p>
        </div>
      </div>

      <div
        v-if="apiForward || extraForwards.length"
        class="mt-3 rounded-xl border border-line bg-surface-2 p-4"
      >
        <p class="text-xs text-ink-faint">
          Reached through Biebie Access<span v-if="gateway"> on {{ gateway }}</span>
        </p>

        <div v-if="apiForward" class="mt-2 grid gap-2 sm:grid-cols-2">
          <div>
            <p class="text-[10px] tracking-wide text-ink-faint">On this machine</p>
            <p class="truncate font-mono text-sm text-brand">
              127.0.0.1:{{ apiForward.localPort }}
            </p>
          </div>
          <div>
            <p class="text-[10px] tracking-wide text-ink-faint">The cluster's own address</p>
            <p class="truncate font-mono text-sm text-ink-muted">{{ apiAddress || '—' }}</p>
          </div>
        </div>

        <p v-if="apiForward" class="mt-2 text-xs leading-relaxed text-ink-muted">
          The loopback port is only an entrance. The certificate is still checked against the
          cluster's own name, so nothing about this connection is less verified than a direct one.
        </p>

        <template v-if="extraForwards.length">
          <p class="mt-3 text-[10px] tracking-wide text-ink-faint">
            Also lent to this machine, not part of the cluster
          </p>
          <div class="mt-1.5 flex flex-wrap gap-2">
            <button
              v-for="forward in extraForwards"
              :key="forward.localPort"
              class="rounded-lg border border-line px-2.5 py-1 text-left transition hover:border-brand/60"
              :title="`Open in your browser. ${remoteOf(forward)}`"
              @click="openInBrowser(`http://127.0.0.1:${forward.localPort}`)"
            >
              <span class="block font-mono text-xs text-ink">
                127.0.0.1:{{ forward.localPort }}
              </span>
              <span class="block text-[10px] text-ink-faint">
                {{ forward.name || remoteOf(forward) }}
              </span>
            </button>
          </div>
        </template>
      </div>

      <div v-if="overview.metrics" class="mt-3 grid gap-3 sm:grid-cols-2">
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <div class="flex items-baseline justify-between">
            <p class="text-xs text-ink-faint">CPU</p>
            <p class="font-mono text-xs text-ink-muted">
              {{ millicores(overview.metrics.cpuUsedMilli) }} /
              {{ millicores(overview.metrics.cpuCapacityMilli) }}
            </p>
          </div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div
              class="h-full rounded-full bg-brand"
              :style="{
                width: `${Math.min(100, (overview.metrics.cpuUsedMilli / Math.max(1, overview.metrics.cpuCapacityMilli)) * 100)}%`,
              }"
            />
          </div>
        </div>
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <div class="flex items-baseline justify-between">
            <p class="text-xs text-ink-faint">Memory</p>
            <p class="font-mono text-xs text-ink-muted">
              {{ bytes(overview.metrics.memoryUsedBytes) }} /
              {{ bytes(overview.metrics.memoryCapacityBytes) }}
            </p>
          </div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div
              class="h-full rounded-full bg-brand"
              :style="{
                width: `${Math.min(100, (overview.metrics.memoryUsedBytes / Math.max(1, overview.metrics.memoryCapacityBytes)) * 100)}%`,
              }"
            />
          </div>
        </div>
      </div>

      <!-- No metrics-server is an ordinary state for an on-premise cluster, so
           it is stated rather than shown as a broken widget. -->
      <p v-else class="mt-3 rounded-xl border border-line bg-surface-2 px-4 py-3 text-xs text-ink-muted">
        This cluster has no metrics-server, so CPU and memory usage are unavailable. Everything else
        works normally.
      </p>

      <section class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
          Recent warnings
        </h2>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <p
            v-if="!overview.recentWarnings?.length"
            class="bg-surface-2 px-4 py-6 text-center text-sm text-ink-muted"
          >
            No warning events. This cluster is quiet.
          </p>
          <ul v-else class="divide-y divide-line">
            <li
              v-for="event in overview.recentWarnings"
              :key="event.uid"
              class="flex items-start gap-3 bg-surface-2 px-4 py-2.5"
            >
              <StateDot :health="Health.HealthWarning" class="mt-1.5" />
              <div class="min-w-0 flex-1">
                <p class="text-sm text-ink">
                  <span class="font-medium">{{ event.reason }}</span>
                  <span class="text-ink-faint"> · {{ event.object }}</span>
                </p>
                <p class="mt-0.5 truncate text-xs text-ink-muted">{{ event.message }}</p>
              </div>
              <span class="shrink-0 font-mono text-xs text-ink-faint">{{ age(event.lastSeen) }}</span>
            </li>
          </ul>
        </div>
      </section>
    </template>
  </div>
</template>
