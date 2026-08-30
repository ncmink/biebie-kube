<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import ContextTrail from '@/components/common/ContextTrail.vue'
import { api, message } from '@/api'
import { currentReplicas } from '@/composables/actions'
import type { ActionDescriptor } from '@/composables/actions'
import { singularTitle } from '@/composables/kind'
import { EnvironmentKind } from '@/types'
import type { Cluster, Kind, ResourceRow } from '@/types'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{
  clusterId: string
  kind: Kind
  kindTitle: string
  row: ResourceRow
  action: ActionDescriptor
  cluster?: Cluster
}>()

const emit = defineEmits<{ close: [] }>()

const ui = useUIStore()

const replicas = ref(0)
const typed = ref('')
const running = ref(false)

const heading = computed(() => singularTitle(props.kindTitle))

/**
 * Production asks for the object's name, exactly as a delete does.
 *
 * The rule is deliberately the same for every action rather than tuned per
 * verb: a restart is as disruptive as a scale to zero, and a guard an engineer
 * has to reason about is one they will eventually reason their way around.
 */
const confirmName = computed(() =>
  props.cluster?.environmentKind === EnvironmentKind.EnvironmentProduction ? props.row.name : '',
)

// An emptied number input holds "" rather than a number, and sending that to a
// Go int32 fails at the binding with an error about JSON rather than about the
// blank field the user is looking at.
const counted = computed(
  () => !props.action.replicas || (Number.isInteger(replicas.value) && replicas.value >= 0),
)

const ready = computed(
  () => counted.value && (!confirmName.value || typed.value.trim() === confirmName.value),
)

watch(
  () => [props.action.action, props.row.name],
  () => {
    replicas.value = currentReplicas(props.row)
    typed.value = ''
  },
  { immediate: true },
)

async function run() {
  if (!ready.value || running.value) return
  running.value = true
  try {
    const result = await api.performAction(props.clusterId, {
      ref: { kind: props.kind, namespace: props.row.namespace, name: props.row.name },
      action: props.action.action,
      replicas: replicas.value,
    })
    ui.say(result.message)
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
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-md rounded-2xl border border-line bg-surface-2 p-6 shadow-2xl">
      <h2 class="text-base font-semibold text-ink">
        {{ action.verb }} {{ heading }} “{{ row.name }}”?
      </h2>
      <p class="mt-2 text-sm leading-relaxed text-ink-muted">{{ action.detail }}</p>

      <div v-if="cluster" class="mt-4 rounded-xl border border-line bg-surface-3 px-3 py-2.5">
        <p class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
          This will run against
        </p>
        <ContextTrail :cluster="cluster" compact class="mt-1.5" />
        <p v-if="row.namespace" class="mt-1.5 font-mono text-[11px] text-ink-faint">
          namespace {{ row.namespace }}
        </p>
      </div>

      <label v-if="action.replicas" class="mt-4 block">
        <span class="text-xs text-ink-muted">Replicas</span>
        <input
          v-model.number="replicas"
          type="number"
          min="0"
          class="mt-1.5 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
          @keydown.enter="run"
        />
        <span v-if="replicas === 0" class="mt-1.5 block text-xs text-warn">
          Zero replicas stops this workload. It stays in the cluster and serves nothing.
        </span>
      </label>

      <label v-if="confirmName" class="mt-4 block">
        <span class="text-xs text-warn">
          This is production. Type
          <span class="font-mono text-ink">{{ confirmName }}</span> to confirm
        </span>
        <input
          v-model="typed"
          class="mt-1.5 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
          autocomplete="off"
          spellcheck="false"
          @keydown.enter="run"
        />
      </label>

      <div class="mt-6 flex justify-end gap-2">
        <button
          class="rounded-lg border border-line px-3 py-1.5 text-sm text-ink-muted hover:text-ink"
          @click="emit('close')"
        >
          Cancel
        </button>
        <button
          class="rounded-lg bg-brand px-3 py-1.5 text-sm font-semibold text-surface-1 disabled:cursor-not-allowed disabled:opacity-40"
          :disabled="!ready || running"
          @click="run"
        >
          {{ running ? 'Working…' : action.verb }}
        </button>
      </div>
    </div>
  </div>
</template>
