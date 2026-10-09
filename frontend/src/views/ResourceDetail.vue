<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import StateDot from '@/components/common/StateDot.vue'
import EventList from '@/components/resource/EventList.vue'
import IncidentPanel from '@/components/resource/IncidentPanel.vue'
import LogViewer from '@/components/logs/LogViewer.vue'
import PodDetails from '@/components/workload/PodDetails.vue'
import PortForwardDialog from '@/components/workload/PortForwardDialog.vue'
import { api, message } from '@/api'
import { age } from '@/composables/format'
import { asKind, singularTitle } from '@/composables/kind'
import { EnvironmentKind, Health, Kind } from '@/types'
import type { ContainerPort, PodDetail, ResourceRef } from '@/types'

// Monaco and xterm are each larger than the rest of the application together.
// Loading them with the tab that needs them keeps opening a pod instant for the
// common case, which is reading its overview or logs.
const PodTerminal = defineAsyncComponent(() => import('@/components/terminal/PodTerminal.vue'))
const YamlEditor = defineAsyncComponent(() => import('@/components/yaml/YamlEditor.vue'))
import { useResourceDetailShortcuts } from '@/composables/useResourceDetailShortcuts'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{ clusterId: string; kind: string; namespace: string; name: string }>()

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()
const route = useRoute()

// "_" stands in for "no namespace" in the route, since a cluster-scoped object
// still needs a path segment.
const realNamespace = computed(() => (props.namespace === '_' ? '' : props.namespace))
const cluster = computed(() => clusters.clusters.find((c) => c.id === props.clusterId))

// The kind arrives from the URL, so it may be anything. An unrecognised kind
// leaves the tabs empty rather than asking the cluster about a type that does
// not exist. What counts as recognised comes from the cluster's catalogue, so
// the operators' own resources are as openable as the built-in ones.
const catalogue = computed(() => clusters.catalogues[props.clusterId] ?? [])
const resourceKind = computed(() => asKind(props.kind, catalogue.value))
const isPod = computed(() => resourceKind.value === Kind.KindPod)
const explainable = computed(() => {
  switch (resourceKind.value) {
    case Kind.KindPod:
    case Kind.KindNode:
    case Kind.KindDeployment:
    case Kind.KindStatefulSet:
    case Kind.KindDaemonSet:
    case Kind.KindJob:
    case Kind.KindPersistentVolumeClaim:
      return true
    default:
      return false
  }
})

const tabs = computed(() => {
  if (!resourceKind.value) return []
  if (isPod.value) return ['Overview', 'Logs', 'Terminal', 'YAML', 'Events', 'Explain']
  if (explainable.value) return ['YAML', 'Events', 'Explain']
  return ['YAML', 'Events']
})

// The catalogue holds the word the engineer wrote in their own manifests, which
// beats trimming an "s" off a route segment — a custom kind's segment is
// "applications.argoproj.io" and has no "s" to trim.
const headingPlural = computed(() => {
  const entry = catalogue.value.find((item) => item.kind === props.kind)
  return entry?.title ?? props.kind
})
const heading = computed(() => singularTitle(headingPlural.value))

function pickTab(): string {
  const requested = typeof route.query.tab === 'string' ? route.query.tab : ''
  if (requested && tabs.value.includes(requested)) {
    return requested
  }
  return tabs.value[0] ?? ''
}

const tab = ref(pickTab())
const deleting = ref(false)
const forwarding = ref(false)
const podDetail = ref<PodDetail | null>(null)
const podPorts = computed<ContainerPort[]>(() => podDetail.value?.ports ?? [])

watch(
  () => [props.clusterId, props.kind, props.namespace, props.name],
  () => {
    tab.value = pickTab()
    void loadPod()
  },
)

watch(
  () => route.query.tab,
  (queryTab) => {
    if (typeof queryTab === 'string' && tabs.value.includes(queryTab)) {
      tab.value = queryTab
    }
  },
)

// A deep link can open before the catalogue has arrived, and for a custom
// resource the tabs only come into existence with it. Without this the page
// would hold the empty selection it started with and render nothing.
watch(tabs, (available) => {
  if (!tab.value || !available.includes(tab.value)) {
    tab.value = pickTab()
  }
})

const ref_ = computed<ResourceRef | undefined>(() =>
  resourceKind.value
    ? { kind: resourceKind.value, namespace: realNamespace.value, name: props.name }
    : undefined,
)

async function loadPod() {
  if (!isPod.value) {
    podDetail.value = null
    return
  }
  try {
    podDetail.value = await api.podDetail(props.clusterId, realNamespace.value, props.name)
  } catch {
    podDetail.value = null
  }
}

onMounted(() => void loadPod())

useResourceDetailShortcuts({
  clusterId: props.clusterId,
  kind: props.kind,
  namespace: props.namespace,
  name: props.name,
  tabs,
  tab,
})

async function remove() {
  deleting.value = false
  if (!ref_.value) return
  try {
    await api.deleteResource(props.clusterId, ref_.value)
    ui.say(`Deleted ${props.name}.`)
    await router.push({ name: 'resources', params: { clusterId: props.clusterId, kind: props.kind } })
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header class="shrink-0 border-b border-line bg-surface-1/40 px-6 pt-3.5 backdrop-blur">
      <!-- Top line: Breadcrumb + Action buttons -->
      <div class="flex items-center justify-between gap-4">
        <nav class="flex items-center gap-1.5 text-xs text-ink-faint">
          <button
            class="inline-flex items-center gap-1 font-medium text-ink-muted transition-colors hover:text-ink"
            @click="router.push({ name: 'resources', params: { clusterId, kind } })"
          >
            <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
              <path
                d="M10 3.5L5.5 8 10 12.5"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span>{{ headingPlural }}</span>
          </button>

          <span class="text-line-strong">/</span>

          <span v-if="realNamespace" class="font-mono text-ink-muted">
            {{ realNamespace }}
          </span>
          <span v-if="realNamespace" class="text-line-strong">/</span>

          <span class="max-w-64 truncate font-mono font-medium text-ink sm:max-w-md">
            {{ name }}
          </span>
        </nav>

        <div class="flex items-center gap-2">
          <button
            v-if="isPod"
            class="inline-flex items-center gap-1.5 rounded-lg border border-line bg-surface-2 px-2.5 py-1 text-xs font-medium text-ink-muted transition hover:border-brand/40 hover:text-ink"
            @click="forwarding = true"
          >
            <svg viewBox="0 0 16 16" class="size-3.5 text-ink-faint" fill="none" aria-hidden="true">
              <path
                d="M3 8h10M9 4l4 4-4 4"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span>Port forward</span>
          </button>

          <button
            class="inline-flex items-center gap-1.5 rounded-lg border border-bad/30 px-2.5 py-1 text-xs font-medium text-bad transition hover:border-bad/60 hover:bg-bad/10"
            @click="deleting = true"
          >
            <svg viewBox="0 0 16 16" class="size-3.5" fill="none" aria-hidden="true">
              <path
                d="M3 4.5h10M6.5 7v4.5M9.5 7v4.5M5 4.5l.5 8.5h5l.5-8.5M6.5 4.5V3h3v1.5"
                stroke="currentColor"
                stroke-width="1.2"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span>Delete</span>
          </button>
        </div>
      </div>

      <!-- Main title and contextual metadata -->
      <div class="mt-2.5 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div class="flex min-w-0 items-center gap-2.5">
          <StateDot
            v-if="isPod && podDetail"
            :health="podDetail.health"
            :pulse="podDetail.health === Health.HealthProgress"
            class="size-2.5 shrink-0"
          />
          <h1 class="truncate font-mono text-base font-bold tracking-tight text-ink">
            {{ name }}
          </h1>
          <span
            v-if="isPod && podDetail?.status"
            class="inline-flex items-center rounded-md border border-line bg-surface-2 px-2 py-0.5 font-mono text-[11px] font-medium text-ink-muted"
          >
            {{ podDetail.status }}
          </span>
          <span
            v-else-if="!isPod"
            class="inline-flex items-center rounded-md border border-line bg-surface-2 px-2 py-0.5 text-[11px] font-medium text-ink-faint"
          >
            {{ heading }}
          </span>
        </div>

        <!-- Quick metadata chips for Pod -->
        <div
          v-if="isPod && podDetail"
          class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-faint"
        >
          <span v-if="podDetail.node" class="flex items-center gap-1.5 font-mono">
            <span class="text-ink-muted">node:</span>
            <span class="text-ink">{{ podDetail.node }}</span>
          </span>
          <span v-if="podDetail.podIp" class="flex items-center gap-1.5 font-mono">
            <span class="text-ink-muted">ip:</span>
            <span class="text-ink">{{ podDetail.podIp }}</span>
          </span>
          <span v-if="podDetail.qosClass" class="flex items-center gap-1.5 font-mono">
            <span class="text-ink-muted">qos:</span>
            <span class="text-ink">{{ podDetail.qosClass }}</span>
          </span>
          <span v-if="podDetail.startedAt" class="flex items-center gap-1.5 font-mono">
            <span class="text-ink-muted">age:</span>
            <span class="text-ink">{{ age(podDetail.startedAt) }}</span>
          </span>
        </div>
      </div>

      <!-- Navigation tabs with bottom border anchor -->
      <nav class="mt-4 -mb-px flex gap-1 overflow-x-auto">
        <button
          v-for="entry in tabs"
          :key="entry"
          class="group relative px-3 py-2 text-xs font-medium transition-colors"
          :class="tab === entry ? 'font-semibold text-ink' : 'text-ink-muted hover:text-ink'"
          @click="tab = entry"
        >
          <span>{{ entry }}</span>
          <span
            class="absolute inset-x-0 bottom-0 h-0.5 rounded-full transition-all"
            :class="tab === entry ? 'bg-brand' : 'bg-transparent group-hover:bg-line-strong'"
          />
        </button>
      </nav>
    </header>

    <div class="min-h-0 flex-1">
      <div v-if="tab === 'Overview' && isPod" class="h-full overflow-y-auto px-6 py-5">
        <PodDetails
          :cluster-id="clusterId"
          :namespace="realNamespace"
          :name="name"
        />
      </div>
      <LogViewer
        v-else-if="tab === 'Logs'"
        :cluster-id="clusterId"
        :namespace="realNamespace"
        :pod="name"
      />
      <PodTerminal
        v-else-if="tab === 'Terminal'"
        :cluster-id="clusterId"
        :namespace="realNamespace"
        :pod="name"
      />
      <YamlEditor
        v-else-if="tab === 'YAML' && ref_"
        :cluster-id="clusterId"
        :resource="ref_"
        :cluster="cluster"
      />
      <EventList
        v-else-if="tab === 'Events'"
        :cluster-id="clusterId"
        :namespace="realNamespace"
        :involving="name"
      />
      <div v-else-if="tab === 'Explain' && ref_" class="h-full overflow-y-auto px-6 py-4">
        <IncidentPanel :cluster-id="clusterId" :resource="ref_" />
      </div>
      <p v-else-if="!catalogue.length" class="px-6 py-10 text-center text-sm text-ink-faint">
        Loading…
      </p>
      <p v-else-if="!resourceKind" class="px-6 py-10 text-center text-sm text-ink-faint">
        “{{ kind }}” is not a resource type this cluster serves.
      </p>
    </div>

    <ConfirmDialog
      :open="deleting"
      :title="`Delete ${heading} “${name}”?`"
      :detail="realNamespace ? `In namespace ${realNamespace}. This cannot be undone.` : 'This cannot be undone.'"
      :cluster="cluster"
      :require-typing="
        cluster?.environmentKind === EnvironmentKind.EnvironmentProduction ? name : undefined
      "
      @cancel="deleting = false"
      @confirm="remove"
    />

    <PortForwardDialog
      v-if="isPod"
      :open="forwarding"
      :cluster-id="clusterId"
      :namespace="realNamespace"
      :pod="name"
      :ports="podPorts"
      @close="forwarding = false"
    />
  </div>
</template>
