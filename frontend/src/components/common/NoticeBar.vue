<script setup lang="ts">
import { useUIStore } from '@/stores/ui'

const ui = useUIStore()
</script>

<template>
  <Transition
    enter-active-class="transition duration-150"
    enter-from-class="translate-y-2 opacity-0"
    leave-active-class="transition duration-150"
    leave-to-class="translate-y-2 opacity-0"
  >
    <div
      v-if="ui.notice"
      class="fixed bottom-5 left-1/2 z-40 max-w-xl -translate-x-1/2 rounded-xl border px-4 py-2.5 shadow-xl"
      :class="
        ui.notice.tone === 'bad'
          ? 'border-bad/40 bg-bad/15 text-ink'
          : 'border-ok/40 bg-ok/15 text-ink'
      "
      role="status"
    >
      <div class="flex items-start gap-3">
        <p class="text-sm leading-relaxed">{{ ui.notice.text }}</p>
        <button class="text-xs text-ink-faint hover:text-ink" @click="ui.dismiss()">Dismiss</button>
      </div>
    </div>
  </Transition>
</template>
