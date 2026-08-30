<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import ConfigData from '@/components/resource/ConfigData.vue'
import EventList from '@/components/resource/EventList.vue'
import RelatedGroups from '@/components/resource/RelatedGroups.vue'
import { api, message } from '@/api'
import { actionsFor } from '@/composables/actions'
import { agoClock } from '@/composables/format'
import { asKind, singularTitle } from '@/composables/kind'
import { Kind } from '@/types'
import type { ResourceInspect, ResourceRef, ResourceRow } from '@/types'
import { useClusterStore } from '@/stores/clusters'

const YamlEditor = defineAsyncComponent(() => import('@/components/yaml/YamlEditor.vue'))

const props = defineProps<{
  clusterId: string
  kind: string
  row: ResourceRow
  kindTitle: string
}>()

// Deleting and the action menu are both raised rather than handled, so the one
// confirmation the list view owns is the only place either can be agreed to.
const emit = defineEmits<{ close: []; menu: [event: MouseEvent]; delete: [] }>()

const clusters = useClusterStore()
const router = useRouter()

const cluster = computed(() => clusters.clusters.find((c) => c.id === props.clusterId))
const resourceKind = computed(() => asKind(props.kind, clusters.catalogues[props.clusterId]))
const heading = computed(() => singularTitle(props.kindTitle || props.kind))

const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)

// The menu itself is the list view's, opened over the row this drawer shows.
// A kind with nothing to offer would open one holding only the delete already
// sitting in this header, so the button stays away.
const hasActions = computed(() => actionsFor(kindInfo.value, props.row).length > 0)

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
const labelsOpen = ref(false)
const annotationsOpen = ref(false)

const labels = computed(() => inspect.value?.labels ?? {})
const annotations = computed(() => inspect.value?.annotations ?? {})
const labelCount = computed(() => Object.keys(labels.value).length)
const annotationCount = computed(() => Object.keys(annotations.value).length)
const labelPairs = computed(() => sortedPairs(labels.value))
const annotationPairs = computed(() => sortedPairs(annotations.value))
const extraProperties = computed(() => inspect.value?.properties ?? [])

function sortedPairs(record: { [_ in string]?: string } | null | undefined): [string, string][] {
  return Object.entries(record ?? {})
    .filter((entry): entry is [string, string] => entry[1] != null)
    .sort(([a], [b]) => a.localeCompare(b))
}

const drawerWidthKey = 'biebie-kube.drawer-width'
const minWidth = 320
const defaultWidth = 448

const width = ref(readWidth())
const dragging = ref(false)

function readWidth(): number {
  const stored = Number(localStorage.getItem(drawerWidthKey))
  return Number.isFinite(stored) && stored >= minWidth ? stored : defaultWidth
}

function maxWidth(): number {
  return Math.max(minWidth, window.innerWidth - 280)
}

function startResize(event: PointerEvent) {
  const handle = event.currentTarget as HTMLElement
  handle.setPointerCapture(event.pointerId)
  dragging.value = true
  const originX = event.clientX
  const originWidth = width.value

  const move = (next: PointerEvent) => {
    // The drawer is docked on the right: dragging the left edge leftward
    // makes it wider.
    width.value = Math.min(maxWidth(), Math.max(minWidth, originWidth + (originX - next.clientX)))
  }
  const stop = (next: PointerEvent) => {
    handle.releasePointerCapture(next.pointerId)
    handle.removeEventListener('pointermove', move)
    handle.removeEventListener('pointerup', stop)
    handle.removeEventListener('pointercancel', stop)
    dragging.value = false
    localStorage.setItem(drawerWidthKey, String(Math.round(width.value)))
  }

  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', stop)
  handle.addEventListener('pointercancel', stop)
}

async function load() {
  if (!ref_.value) return
  loading.value = true
  error.value = ''
  yamlOpen.value = false
  labelsOpen.value = false
  annotationsOpen.value = false
  try {
    inspect.value = await api.inspectResource(props.clusterId, ref_.value)
  } catch (err) {
    error.value = message(err)
    inspect.value = null
  } finally {
    loading.value = false
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
  <aside
    class="relative flex h-full min-h-0 shrink-0 flex-col border-l border-line bg-surface-1"
    :class="dragging ? 'select-none' : ''"
    :style="{ width: `${width}px` }"
  >
    <div
      class="no-drag absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize touch-none hover:bg-brand/40"
      :class="dragging ? 'bg-brand/50' : ''"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize drawer"
      @pointerdown.prevent="startResize"
    />
    <header class="flex shrink-0 items-center gap-2 border-b border-line px-4 py-3">
      <h1 class="min-w-0 truncate text-sm font-semibold text-ink">
        {{ heading }}: {{ row.name }}
      </h1>
      <div class="ml-auto flex items-center gap-1">
        <button
          v-if="hasActions"
          class="rounded-lg p-1.5 text-ink-faint hover:bg-surface-3 hover:text-ink"
          title="Actions"
          @click="emit('menu', $event)"
        >
          <svg viewBox="0 0 24 24" class="size-4" fill="currentColor" aria-hidden="true">
            <circle cx="5" cy="12" r="1.6" />
            <circle cx="12" cy="12" r="1.6" />
            <circle cx="19" cy="12" r="1.6" />
          </svg>
        </button>
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
          @click="emit('delete')"
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
          <dl class="mt-3 space-y-2.5 text-xs">
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Created</dt>
              <dd class="text-ink">{{ agoClock(inspect?.createdAt ?? row.createdAt) }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Name</dt>
              <dd class="truncate font-mono text-ink">{{ row.name }}</dd>
            </div>
            <div v-if="row.namespace" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Namespace</dt>
              <dd class="truncate font-mono text-ink">{{ row.namespace }}</dd>
            </div>
            <div v-if="inspect?.type || row.fields?.type" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Type</dt>
              <dd class="text-ink">{{ inspect?.type || row.fields?.type }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Labels</dt>
              <dd>
                <button class="text-brand hover:underline" @click="labelsOpen = !labelsOpen">
                  {{ labelCount }} {{ labelCount === 1 ? 'Label' : 'Labels' }}
                </button>
                <ul v-if="labelsOpen" class="mt-1.5 space-y-1">
                  <li v-for="[key, value] in labelPairs" :key="key" class="break-all font-mono text-ink-muted">
                    {{ key }}={{ value }}
                  </li>
                </ul>
              </dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Annotations</dt>
              <dd>
                <button class="text-brand hover:underline" @click="annotationsOpen = !annotationsOpen">
                  {{ annotationCount }} {{ annotationCount === 1 ? 'Annotation' : 'Annotations' }}
                </button>
                <ul v-if="annotationsOpen" class="mt-1.5 space-y-1">
                  <li
                    v-for="[key, value] in annotationPairs"
                    :key="key"
                    class="break-all font-mono text-ink-muted"
                  >
                    {{ key }}={{ value }}
                  </li>
                </ul>
              </dd>
            </div>
            <div
              v-for="prop in extraProperties"
              :key="prop.label"
              class="grid grid-cols-[8.5rem_1fr] gap-2"
            >
              <dt class="text-ink-faint">{{ prop.label }}</dt>
              <dd
                class="whitespace-pre-wrap break-all text-ink"
                :class="prop.mono ? 'font-mono' : ''"
              >
                {{ prop.value }}
              </dd>
            </div>
          </dl>
        </section>

        <RelatedGroups v-if="ref_" class="mt-6" :cluster-id="clusterId" :resource="ref_" />

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
  </aside>
</template>
