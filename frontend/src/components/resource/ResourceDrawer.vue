<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConfigData from '@/components/resource/ConfigData.vue'
import EventList from '@/components/resource/EventList.vue'
import { api, message } from '@/api'
import { agoClock } from '@/composables/format'
import { asKind, singularTitle } from '@/composables/kind'
import { EnvironmentKind, Kind } from '@/types'
import type { ResourceInspect, ResourceRef, ResourceRow } from '@/types'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'

const YamlEditor = defineAsyncComponent(() => import('@/components/yaml/YamlEditor.vue'))

const props = defineProps<{
  clusterId: string
  kind: string
  row: ResourceRow
  kindTitle: string
}>()

const emit = defineEmits<{ close: [] }>()

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()

const cluster = computed(() => clusters.clusters.find((c) => c.id === props.clusterId))
const resourceKind = computed(() => asKind(props.kind))
const heading = computed(() => singularTitle(props.kindTitle || props.kind))

const ref_ = computed<ResourceRef | undefined>(() =>
  resourceKind.value
    ? { kind: resourceKind.value, namespace: props.row.namespace, name: props.row.name }
    : undefined,
)

const isConfig = computed(
  () => resourceKind.value === Kind.KindSecret || resourceKind.value === Kind.KindConfigMap,
)
const isSecret = computed(() => resourceKind.value === Kind.KindSecret)
const isPod = computed(() => resourceKind.value === Kind.KindPod)

const inspect = ref<ResourceInspect | null>(null)
const error = ref('')
const loading = ref(false)
const yamlOpen = ref(false)
const deleting = ref(false)

async function load() {
  if (!ref_.value) return
  loading.value = true
  error.value = ''
  yamlOpen.value = false
  try {
    inspect.value = await api.inspectResource(props.clusterId, ref_.value)
  } catch (err) {
    error.value = message(err)
    inspect.value = null
  } finally {
    loading.value = false
  }
}

async function remove() {
  deleting.value = false
  if (!ref_.value) return
  try {
    await api.deleteResource(props.clusterId, ref_.value)
    ui.say(`Deleted ${props.row.name}.`)
    emit('close')
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
watch(() => [props.kind, props.row.name, props.row.namespace], load, { immediate: true })
</script>

<template>
  <aside class="flex h-full min-h-0 w-[28rem] shrink-0 flex-col border-l border-line bg-surface-1">
    <header class="flex shrink-0 items-center gap-2 border-b border-line px-4 py-3">
      <h1 class="min-w-0 truncate text-sm font-semibold text-ink">
        {{ heading }}: {{ row.name }}
      </h1>
      <div class="ml-auto flex items-center gap-1">
        <button
          class="rounded-lg p-1.5 text-ink-faint hover:bg-surface-3 hover:text-ink"
          title="Edit YAML"
          @click="yamlOpen = !yamlOpen"
        >
          <svg viewBox="0 0 24 24" class="size-4" fill="none" aria-hidden="true">
            <path
              d="M4 20h4l10-10-4-4L4 16v4zM14 6l4 4"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linejoin="round"
            />
          </svg>
        </button>
        <button
          class="rounded-lg p-1.5 text-ink-faint hover:bg-bad/15 hover:text-bad"
          title="Delete"
          @click="deleting = true"
        >
          <svg viewBox="0 0 24 24" class="size-4" fill="none" aria-hidden="true">
            <path
              d="M5 7h14M10 7V5h4v2M8 7l.8 12h6.4L16 7"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
        <button
          class="rounded-lg p-1.5 text-ink-faint hover:bg-surface-3 hover:text-ink"
          title="Close"
          @click="emit('close')"
        >
          <svg viewBox="0 0 24 24" class="size-4" fill="none" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </div>
    </header>

    <div v-if="yamlOpen && ref_" class="min-h-0 flex-1">
      <YamlEditor :cluster-id="clusterId" :resource="ref_" :cluster="cluster" />
    </div>

    <div v-else class="min-h-0 flex-1 overflow-y-auto px-4 py-4">
      <p v-if="error" class="rounded-xl border border-bad/40 bg-bad/10 px-3 py-2 text-xs text-bad">
        {{ error }}
      </p>
      <p v-else-if="loading" class="text-xs text-ink-muted">Loading…</p>

      <template v-else>
        <section>
          <h2 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
            Properties
          </h2>
          <dl class="mt-3 space-y-2 text-xs">
            <div class="grid grid-cols-[7rem_1fr] gap-2">
              <dt class="text-ink-faint">Created</dt>
              <dd class="text-ink">{{ agoClock(inspect?.createdAt ?? row.createdAt) }}</dd>
            </div>
            <div class="grid grid-cols-[7rem_1fr] gap-2">
              <dt class="text-ink-faint">Name</dt>
              <dd class="truncate font-mono text-ink">{{ row.name }}</dd>
            </div>
            <div v-if="row.namespace" class="grid grid-cols-[7rem_1fr] gap-2">
              <dt class="text-ink-faint">Namespace</dt>
              <dd class="truncate font-mono text-ink">{{ row.namespace }}</dd>
            </div>
            <div v-if="inspect?.type || row.fields?.type" class="grid grid-cols-[7rem_1fr] gap-2">
              <dt class="text-ink-faint">Type</dt>
              <dd class="text-ink">{{ inspect?.type || row.fields?.type }}</dd>
            </div>
          </dl>
        </section>

        <section class="mt-6">
          <h2 class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
            Events
          </h2>
          <div class="max-h-48 overflow-hidden rounded-lg border border-line">
            <EventList
              compact
              :cluster-id="clusterId"
              :namespace="row.namespace ?? ''"
              :involving="row.name"
            />
          </div>
        </section>

        <ConfigData
          v-if="isConfig"
          class="mt-6"
          :entries="inspect?.data ?? []"
          :sensitive="isSecret"
        />

        <button
          v-if="isPod"
          class="mt-6 rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
          @click="
            router.push({
              name: 'resource',
              params: {
                clusterId,
                kind,
                namespace: row.namespace || '_',
                name: row.name,
              },
            })
          "
        >
          Open logs and terminal
        </button>
      </template>
    </div>

    <ConfirmDialog
      :open="deleting"
      :title="`Delete ${heading} “${row.name}”?`"
      :detail="row.namespace ? `In namespace ${row.namespace}. This cannot be undone.` : 'This cannot be undone.'"
      :cluster="cluster"
      :require-typing="
        cluster?.environmentKind === EnvironmentKind.EnvironmentProduction ? row.name : undefined
      "
      @cancel="deleting = false"
      @confirm="remove"
    />
  </aside>
</template>
