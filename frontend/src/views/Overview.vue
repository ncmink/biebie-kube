<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import StateDot from '@/components/common/StateDot.vue'
import { api, message, openInBrowser } from '@/api'
import { age, bytes, millicores } from '@/composables/format'
import { Health } from '@/types'
import type { ClusterOverview, Forward, NodeCapacity } from '@/types'
import { useClusterStore } from '@/stores/clusters'

const props = defineProps<{ clusterId: string }>()

const router = useRouter()
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

/** Denominator for usage bars: allocatable when known, else capacity. */
function cpuTotal(metrics: NonNullable<ClusterOverview['metrics']>) {
  return metrics.cpuAllocatableMilli || metrics.cpuCapacityMilli
}

function memoryTotal(metrics: NonNullable<ClusterOverview['metrics']>) {
  return metrics.memoryAllocatableBytes || metrics.memoryCapacityBytes
}

function percent(used: number, total: number) {
  if (total <= 0) return 0
  return Math.min(100, (used / total) * 100)
}

function barClass(pct: number) {
  if (pct >= 90) return 'bg-bad'
  if (pct >= 70) return 'bg-warn'
  return 'bg-brand'
}

function nodePressure(node: NodeCapacity) {
  return Math.max(
    percent(node.cpuRequestMilli, node.cpuAllocatableMilli),
    percent(node.memoryRequestBytes, node.memoryAllocatableBytes),
    percent(node.podsUsed, node.maxPods),
  )
}

const sortedNodes = computed(() => {
  const nodes = overview.value?.nodeCapacity ?? []
  return [...nodes].sort((left, right) => nodePressure(right) - nodePressure(left))
})

function nodeStatus(node: NodeCapacity) {
  if (!node.ready) return 'NotReady'
  if (node.cordoned) return 'Cordoned'
  return 'Ready'
}

function openNode(node: NodeCapacity) {
  void router.push({
    name: 'resource',
    params: {
      clusterId: props.clusterId,
      kind: 'nodes',
      namespace: '_',
      name: node.name,
    },
  })
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
              {{ millicores(cpuTotal(overview.metrics)) }}
            </p>
          </div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div
              class="h-full rounded-full"
              :class="barClass(percent(overview.metrics.cpuUsedMilli, cpuTotal(overview.metrics)))"
              :style="{
                width: `${percent(overview.metrics.cpuUsedMilli, cpuTotal(overview.metrics))}%`,
              }"
            />
          </div>
        </div>
        <div class="rounded-xl border border-line bg-surface-2 p-4">
          <div class="flex items-baseline justify-between">
            <p class="text-xs text-ink-faint">Memory</p>
            <p class="font-mono text-xs text-ink-muted">
              {{ bytes(overview.metrics.memoryUsedBytes) }} /
              {{ bytes(memoryTotal(overview.metrics)) }}
            </p>
          </div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div
              class="h-full rounded-full"
              :class="barClass(percent(overview.metrics.memoryUsedBytes, memoryTotal(overview.metrics)))"
              :style="{
                width: `${percent(overview.metrics.memoryUsedBytes, memoryTotal(overview.metrics))}%`,
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

      <section v-if="overview.nodeCapacity?.length" class="mt-6">
        <div class="flex items-baseline justify-between gap-3">
          <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
            Node capacity
          </h2>
          <p v-if="overview.unscheduledPods" class="text-xs text-warn">
            {{ overview.unscheduledPods }} pod{{ overview.unscheduledPods === 1 ? '' : 's' }} waiting
            for a node
          </p>
        </div>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <div
            class="hidden grid-cols-[minmax(0,1.4fr)_repeat(5,minmax(0,1fr))] gap-3 border-b border-line bg-surface-3 px-4 py-2 text-[10px] uppercase tracking-wide text-ink-faint lg:grid"
          >
            <span>Node</span>
            <span>CPU used</span>
            <span>CPU req</span>
            <span>Mem used</span>
            <span>Mem req</span>
            <span>Pods</span>
          </div>
          <ul class="divide-y divide-line">
            <li
              v-for="node in sortedNodes"
              :key="node.name"
              class="grid gap-3 bg-surface-2 px-4 py-3 lg:grid-cols-[minmax(0,1.4fr)_repeat(5,minmax(0,1fr))]"
            >
              <button
                class="min-w-0 text-left"
                :title="`Open ${node.name}`"
                @click="openNode(node)"
              >
                <span class="block truncate font-mono text-sm text-brand hover:underline">
                  {{ node.name }}
                </span>
                <span class="mt-0.5 block text-[10px] text-ink-faint">{{ nodeStatus(node) }}</span>
              </button>
              <div>
                <p class="font-mono text-xs text-ink-muted">
                  {{ millicores(node.cpuUsedMilli ?? 0) }} /
                  {{ millicores(node.cpuAllocatableMilli) }}
                </p>
                <div class="mt-1 h-1 overflow-hidden rounded-full bg-surface-3">
                  <div
                    class="h-full rounded-full"
                    :class="barClass(percent(node.cpuUsedMilli ?? 0, node.cpuAllocatableMilli))"
                    :style="{
                      width: `${percent(node.cpuUsedMilli ?? 0, node.cpuAllocatableMilli)}%`,
                    }"
                  />
                </div>
              </div>
              <div>
                <p class="font-mono text-xs text-ink-muted">
                  {{ millicores(node.cpuRequestMilli) }}
                  ({{ percent(node.cpuRequestMilli, node.cpuAllocatableMilli).toFixed(0) }}%)
                </p>
                <div class="mt-1 h-1 overflow-hidden rounded-full bg-surface-3">
                  <div
                    class="h-full rounded-full"
                    :class="barClass(percent(node.cpuRequestMilli, node.cpuAllocatableMilli))"
                    :style="{
                      width: `${percent(node.cpuRequestMilli, node.cpuAllocatableMilli)}%`,
                    }"
                  />
                </div>
              </div>
              <div>
                <p class="font-mono text-xs text-ink-muted">
                  {{ bytes(node.memoryUsedBytes ?? 0) }} /
                  {{ bytes(node.memoryAllocatableBytes) }}
                </p>
                <div class="mt-1 h-1 overflow-hidden rounded-full bg-surface-3">
                  <div
                    class="h-full rounded-full"
                    :class="barClass(percent(node.memoryUsedBytes ?? 0, node.memoryAllocatableBytes))"
                    :style="{
                      width: `${percent(node.memoryUsedBytes ?? 0, node.memoryAllocatableBytes)}%`,
                    }"
                  />
                </div>
              </div>
              <div>
                <p class="font-mono text-xs text-ink-muted">
                  {{ bytes(node.memoryRequestBytes) }}
                  ({{ percent(node.memoryRequestBytes, node.memoryAllocatableBytes).toFixed(0) }}%)
                </p>
                <div class="mt-1 h-1 overflow-hidden rounded-full bg-surface-3">
                  <div
                    class="h-full rounded-full"
                    :class="barClass(percent(node.memoryRequestBytes, node.memoryAllocatableBytes))"
                    :style="{
                      width: `${percent(node.memoryRequestBytes, node.memoryAllocatableBytes)}%`,
                    }"
                  />
                </div>
              </div>
              <div>
                <p class="font-mono text-xs text-ink-muted">
                  {{ node.podsUsed }}/{{ node.maxPods }}
                  ({{ Math.max(0, node.maxPods - node.podsUsed) }} free)
                </p>
                <div class="mt-1 h-1 overflow-hidden rounded-full bg-surface-3">
                  <div
                    class="h-full rounded-full"
                    :class="barClass(percent(node.podsUsed, node.maxPods))"
                    :style="{ width: `${percent(node.podsUsed, node.maxPods)}%` }"
                  />
                </div>
              </div>
            </li>
          </ul>
        </div>
      </section>

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
