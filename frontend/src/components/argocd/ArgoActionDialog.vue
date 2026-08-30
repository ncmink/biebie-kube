<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import ContextTrail from '@/components/common/ContextTrail.vue'
import StateDot from '@/components/common/StateDot.vue'
import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind } from '@/types'
import type { ArgoApp, ArgoAppRef, Cluster } from '@/types'

const props = defineProps<{
  open: boolean
  mode: 'sync' | 'refresh'
  clusterId: string
  cluster?: Cluster
  /** only scopes the dialog to one Application, for a row's own action. */
  only?: ArgoAppRef
}>()

const emit = defineEmits<{ close: []; done: [] }>()

const ui = useUIStore()

const apps = ref<ArgoApp[]>([])
const chosen = ref(new Set<string>())
const loading = ref(false)
const running = ref(false)
const error = ref('')

const prune = ref(false)
const hard = ref(false)
const typed = ref('')

const key = (app: ArgoAppRef) => `${app.namespace}/${app.name}`
const outOfSync = (app: ArgoApp) => (app.sync ?? '').toLowerCase() === 'outofsync'

/**
 * A sync leads with the Applications that are behind Git, because those are
 * the ones it preselected and a selection the engineer cannot see is a
 * selection they cannot check. A refresh takes every Application anyway, so it
 * keeps the order the backend sent — worst first.
 */
const ordered = computed(() => {
  if (props.mode !== 'sync') return apps.value
  return [...apps.value].sort((a, b) => Number(outOfSync(b)) - Number(outOfSync(a)))
})

const selected = computed(() => ordered.value.filter((app) => chosen.value.has(key(app))))

/**
 * Pruning against production is the one combination worth slowing down: it
 * deletes live resources, and the expensive mistake is doing it in the right
 * cluster for the wrong customer.
 */
const confirmName = computed(() =>
  props.mode === 'sync' &&
  prune.value &&
  props.cluster?.environmentKind === EnvironmentKind.EnvironmentProduction
    ? props.cluster.name
    : '',
)

const ready = computed(
  () => selected.value.length > 0 && (!confirmName.value || typed.value.trim() === confirmName.value),
)

const title = computed(() =>
  props.mode === 'sync' ? 'Sync applications' : 'Refresh applications',
)

async function load() {
  loading.value = true
  error.value = ''
  prune.value = false
  hard.value = false
  typed.value = ''
  try {
    const all = await api.argoApplications(props.clusterId)
    apps.value = props.only ? all.filter((app) => key(app) === key(props.only!)) : all

    // A sync preselects what is out of sync; a refresh is cheap and asks for
    // everything, which is what makes it the thing to reach for after a push.
    const preselected = apps.value.filter(
      (app) => props.mode === 'refresh' || outOfSync(app) || props.only,
    )
    chosen.value = new Set(preselected.map(key))
  } catch (err) {
    error.value = message(err)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) void load()
  },
  { immediate: true },
)

function toggle(app: ArgoApp) {
  const next = new Set(chosen.value)
  if (!next.delete(key(app))) next.add(key(app))
  chosen.value = next
}

function toggleAll() {
  chosen.value =
    chosen.value.size === ordered.value.length ? new Set() : new Set(ordered.value.map(key))
}

/**
 * One notice covers the whole batch.
 *
 * Forty Applications syncing where two are refused is neither a success nor a
 * failure, so both halves are reported in one line rather than as forty
 * toasts the engineer would dismiss without reading.
 */
async function run() {
  if (!ready.value || running.value) return
  running.value = true
  const refs: ArgoAppRef[] = selected.value.map((app) => ({
    namespace: app.namespace,
    name: app.name,
  }))

  try {
    const result =
      props.mode === 'sync'
        ? await api.syncArgoApps(props.clusterId, refs, prune.value)
        : await api.refreshArgoApps(props.clusterId, refs, hard.value)

    const done = result.succeeded?.length ?? 0
    const failed = result.failed ?? []
    const verb = props.mode === 'sync' ? 'Requested a sync for' : 'Refreshed'

    if (failed.length) {
      ui.say(
        `${verb} ${done} application${done === 1 ? '' : 's'}. ${failed.length} failed: ${failed
          .map((entry) => `${entry.app} (${entry.error})`)
          .join('; ')}`,
        'bad',
      )
    } else {
      ui.say(`${verb} ${done} application${done === 1 ? '' : 's'}.`)
    }
    emit('done')
    emit('close')
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
    @click.self="emit('close')"
  >
    <div class="flex max-h-[80vh] w-full max-w-2xl flex-col rounded-2xl border border-line bg-surface-2 shadow-2xl">
      <header class="shrink-0 border-b border-line px-6 py-4">
        <h2 class="text-base font-semibold text-ink">{{ title }}</h2>
        <div v-if="cluster" class="mt-2 rounded-xl border border-line bg-surface-3 px-3 py-2.5">
          <p class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
            This will run against
          </p>
          <ContextTrail :cluster="cluster" compact class="mt-1.5" />
        </div>
      </header>

      <div class="min-h-0 flex-1 overflow-y-auto px-6 py-3">
        <p v-if="error" class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
          {{ error }}
        </p>
        <p v-else-if="loading" class="py-6 text-sm text-ink-muted">Reading applications…</p>
        <p v-else-if="!ordered.length" class="py-6 text-sm text-ink-muted">
          This cluster has no Argo CD applications.
        </p>

        <template v-else>
          <button
            class="text-xs text-ink-muted hover:text-ink"
            @click="toggleAll"
          >
            {{ chosen.size === ordered.length ? 'Clear selection' : 'Select all' }}
            <span class="font-mono text-ink-faint">({{ chosen.size }}/{{ ordered.length }})</span>
          </button>

          <ul class="mt-2 divide-y divide-line overflow-hidden rounded-xl border border-line">
            <li v-for="app in ordered" :key="`${app.namespace}/${app.name}`">
              <label class="flex cursor-pointer items-center gap-3 bg-surface-2 px-3 py-2 hover:bg-surface-3">
                <input
                  type="checkbox"
                  class="size-3.5 shrink-0 accent-brand"
                  :checked="chosen.has(`${app.namespace}/${app.name}`)"
                  @change="toggle(app)"
                />
                <StateDot :health="app.health" />
                <span class="min-w-0 flex-1 truncate text-sm text-ink">{{ app.name }}</span>
                <span class="shrink-0 truncate font-mono text-[11px] text-ink-faint">
                  {{ app.namespace }}
                </span>
                <span class="w-24 shrink-0 truncate text-right text-xs text-ink-muted">
                  {{ app.sync || '—' }}
                </span>
                <span class="w-24 shrink-0 truncate text-right text-xs text-ink-muted">
                  {{ app.healthStatus || '—' }}
                </span>
              </label>
            </li>
          </ul>
        </template>
      </div>

      <footer class="shrink-0 border-t border-line px-6 py-4">
        <label v-if="mode === 'sync'" class="flex items-start gap-2.5">
          <input v-model="prune" type="checkbox" class="mt-0.5 size-3.5 shrink-0 accent-warn" />
          <span class="text-xs">
            <span class="text-ink">Prune resources not in Git</span>
            <span class="block text-ink-muted">
              Deletes live resources that no longer exist in the target revision. Leave this off
              unless you intend to remove them.
            </span>
          </span>
        </label>

        <div v-else class="flex items-center gap-4 text-xs">
          <label class="flex items-center gap-1.5">
            <input v-model="hard" type="radio" :value="false" class="size-3.5 accent-brand" />
            <span class="text-ink">Normal</span>
          </label>
          <label class="flex items-center gap-1.5">
            <input v-model="hard" type="radio" :value="true" class="size-3.5 accent-brand" />
            <span class="text-ink">Hard</span>
          </label>
          <span class="text-ink-muted">
            {{
              hard
                ? 'Re-compares and discards the manifest cache.'
                : 'Re-compares against the cached manifests.'
            }}
          </span>
        </div>

        <label v-if="confirmName" class="mt-3 block">
          <span class="text-xs text-warn">
            Pruning in production. Type
            <span class="font-mono text-ink">{{ confirmName }}</span> to confirm
          </span>
          <input
            v-model="typed"
            class="mt-1.5 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
            autocomplete="off"
            spellcheck="false"
          />
        </label>

        <div class="mt-4 flex items-center justify-end gap-2">
          <button
            class="rounded-lg border border-line px-3 py-1.5 text-sm text-ink-muted hover:text-ink"
            @click="emit('close')"
          >
            Cancel
          </button>
          <button
            class="rounded-lg px-3 py-1.5 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-40"
            :class="prune ? 'bg-warn text-surface-1' : 'bg-brand text-surface-1'"
            :disabled="!ready || running"
            @click="run"
          >
            {{ running ? 'Working…' : mode === 'sync' ? 'Sync' : 'Refresh' }}
            {{ selected.length ? `(${selected.length})` : '' }}
          </button>
        </div>
      </footer>
    </div>
  </div>
</template>
