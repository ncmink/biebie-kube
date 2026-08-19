<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import ContextTrail from './ContextTrail.vue'
import type { Cluster } from '@/types'

const props = defineProps<{
  open: boolean
  title: string
  detail?: string
  cluster?: Cluster
  confirmLabel?: string
  /**
   * requireTyping asks the user to type a name before the action is enabled.
   * It is reserved for production, where a reflexive click is the whole risk.
   */
  requireTyping?: string
}>()

const emit = defineEmits<{ confirm: []; cancel: [] }>()

const typed = ref('')

watch(
  () => props.open,
  (open) => {
    if (open) typed.value = ''
  },
)

const ready = computed(() => !props.requireTyping || typed.value.trim() === props.requireTyping)
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
    @click.self="emit('cancel')"
  >
    <div class="w-full max-w-md rounded-2xl border border-line bg-surface-2 p-6 shadow-2xl">
      <h2 class="text-base font-semibold text-ink">{{ title }}</h2>
      <p v-if="detail" class="mt-2 text-sm leading-relaxed text-ink-muted">{{ detail }}</p>

      <div v-if="cluster" class="mt-4 rounded-xl border border-line bg-surface-3 px-3 py-2.5">
        <p class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
          This will run against
        </p>
        <ContextTrail :cluster="cluster" compact class="mt-1.5" />
      </div>

      <label v-if="requireTyping" class="mt-4 block">
        <span class="text-xs text-ink-muted">
          Type <span class="font-mono text-ink">{{ requireTyping }}</span> to confirm
        </span>
        <input
          v-model="typed"
          class="mt-1.5 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-sm text-ink outline-none focus:border-brand"
          autocomplete="off"
          spellcheck="false"
        />
      </label>

      <div class="mt-6 flex justify-end gap-2">
        <button
          class="rounded-lg border border-line px-3 py-1.5 text-sm text-ink-muted hover:text-ink"
          @click="emit('cancel')"
        >
          Cancel
        </button>
        <button
          class="rounded-lg bg-bad px-3 py-1.5 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:opacity-40"
          :disabled="!ready"
          @click="emit('confirm')"
        >
          {{ confirmLabel ?? 'Delete' }}
        </button>
      </div>
    </div>
  </div>
</template>
