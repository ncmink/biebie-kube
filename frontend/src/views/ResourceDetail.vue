<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EventList from '@/components/resource/EventList.vue'
import LogViewer from '@/components/logs/LogViewer.vue'
import PodOverview from '@/components/workload/PodOverview.vue'
import PortForwardDialog from '@/components/workload/PortForwardDialog.vue'
import { api, message } from '@/api'
import { asKind } from '@/composables/kind'
import { EnvironmentKind, Kind } from '@/types'
import type { ResourceRef } from '@/types'

// Monaco and xterm are each larger than the rest of the application together.
// Loading them with the tab that needs them keeps opening a pod instant for the
// common case, which is reading its overview or its logs.
const PodTerminal = defineAsyncComponent(() => import('@/components/terminal/PodTerminal.vue'))
const YamlEditor = defineAsyncComponent(() => import('@/components/yaml/YamlEditor.vue'))
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{ clusterId: string; kind: string; namespace: string; name: string }>()

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()

// "_" stands in for "no namespace" in the route, since a cluster-scoped object
// still needs a path segment.
const realNamespace = computed(() => (props.namespace === '_' ? '' : props.namespace))
const cluster = computed(() => clusters.clusters.find((c) => c.id === props.clusterId))

// The kind arrives from the URL, so it may be anything. An unrecognised kind
// leaves the tabs empty rather than asking the cluster about a type that does
// not exist.
const resourceKind = computed(() => asKind(props.kind))
const isPod = computed(() => resourceKind.value === Kind.KindPod)

const tabs = computed(() => {
  if (!resourceKind.value) return []
  return isPod.value ? ['Overview', 'Logs', 'Terminal', 'YAML', 'Events'] : ['YAML', 'Events']
})
const tab = ref(tabs.value[0])
const deleting = ref(false)
const forwarding = ref(false)

watch(
  () => [props.kind, props.name],
  () => {
    tab.value = tabs.value[0]
  },
)

const ref_ = computed<ResourceRef | undefined>(() =>
  resourceKind.value
    ? { kind: resourceKind.value, namespace: realNamespace.value, name: props.name }
    : undefined,
)

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
    <header class="shrink-0 border-b border-line px-6 py-3">
      <div class="flex items-center gap-3">
        <button
          class="text-xs text-ink-faint hover:text-ink"
          @click="router.push({ name: 'resources', params: { clusterId, kind } })"
        >
          ← Back
        </button>
        <h1 class="truncate text-sm font-semibold text-ink">{{ name }}</h1>
        <span v-if="realNamespace" class="truncate font-mono text-xs text-ink-faint">
          {{ realNamespace }}
        </span>

        <div class="ml-auto flex gap-2">
          <button
            v-if="isPod"
            class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
            @click="forwarding = true"
          >
            Port forward
          </button>
          <button
            class="rounded-lg border border-bad/40 px-2.5 py-1 text-xs text-bad hover:bg-bad/10"
            @click="deleting = true"
          >
            Delete
          </button>
        </div>
      </div>

      <nav class="mt-3 flex gap-1">
        <button
          v-for="entry in tabs"
          :key="entry"
          class="rounded-lg px-2.5 py-1 text-xs"
          :class="tab === entry ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-2'"
          @click="tab = entry"
        >
          {{ entry }}
        </button>
      </nav>
    </header>

    <div class="min-h-0 flex-1">
      <PodOverview
        v-if="tab === 'Overview' && isPod"
        :cluster-id="clusterId"
        :namespace="realNamespace"
        :name="name"
      />
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
      <p v-else-if="!resourceKind" class="px-6 py-10 text-center text-sm text-ink-faint">
        “{{ kind }}” is not a resource type Biebie Kube knows about.
      </p>
    </div>

    <ConfirmDialog
      :open="deleting"
      :title="`Delete ${kind.replace(/s$/, '')} “${name}”?`"
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
      @close="forwarding = false"
    />
  </div>
</template>
