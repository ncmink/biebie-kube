<script setup lang="ts">
import { ref, watch } from 'vue'

import { message } from '@/api'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{ open: boolean; clusterId: string; namespace: string; pod: string }>()
const emit = defineEmits<{ close: [] }>()

const forwards = usePortForwardStore()
const ui = useUIStore()

const remotePort = ref<number | null>(null)
const localPort = ref<number | null>(null)
const busy = ref(false)
const error = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open) return
    remotePort.value = null
    localPort.value = null
    error.value = ''
  },
)

async function start() {
  if (!remotePort.value) {
    error.value = 'Enter the port the container listens on.'
    return
  }
  busy.value = true
  error.value = ''
  try {
    const session = await forwards.start(
      props.clusterId,
      props.namespace,
      'pod',
      props.pod,
      remotePort.value,
      // Zero asks the backend to pick a free local port.
      localPort.value ?? 0,
    )
    ui.say(`Forwarding localhost:${session.localPort} to ${props.pod}:${session.remotePort}.`)
    emit('close')
  } catch (err) {
    error.value = message(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-6"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-sm rounded-2xl border border-line bg-surface-2 p-5">
      <h2 class="text-sm font-semibold text-ink">Port forward</h2>
      <p class="mt-1 truncate text-xs text-ink-muted">{{ namespace }} / {{ pod }}</p>

      <div class="mt-4 grid grid-cols-2 gap-3">
        <label class="block">
          <span class="text-xs text-ink-faint">Container port</span>
          <input
            v-model.number="remotePort"
            type="number"
            class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-2.5 py-1.5 text-sm text-ink outline-none focus:border-brand"
            placeholder="3000"
          />
        </label>
        <label class="block">
          <span class="text-xs text-ink-faint">Local port</span>
          <input
            v-model.number="localPort"
            type="number"
            class="mt-1 w-full rounded-lg border border-line bg-surface-1 px-2.5 py-1.5 text-sm text-ink outline-none focus:border-brand"
            placeholder="auto"
          />
        </label>
      </div>

      <p class="mt-2 text-[11px] text-ink-faint">
        The forward listens on 127.0.0.1 only. Nothing else on your network can reach it.
      </p>

      <p v-if="error" class="mt-3 text-xs text-bad">{{ error }}</p>

      <div class="mt-5 flex justify-end gap-2">
        <button
          class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
          @click="emit('close')"
        >
          Cancel
        </button>
        <button
          class="rounded-lg bg-brand px-3 py-1.5 text-xs font-semibold text-surface-1 disabled:opacity-40"
          :disabled="busy"
          @click="start"
        >
          Start
        </button>
      </div>
    </div>
  </div>
</template>
