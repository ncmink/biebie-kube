<script setup lang="ts">
import { computed } from 'vue'

import { EnvironmentKind } from '@/types'

const props = defineProps<{ kind: EnvironmentKind; label?: string }>()

const isProduction = computed(() => props.kind === EnvironmentKind.EnvironmentProduction)

/**
 * Production is spelled out, not merely coloured.
 *
 * A red tint disappears on a projector, on a colour-blind display and in a
 * screenshot pasted into a ticket. The word does not.
 */
const text = computed(() => {
  if (isProduction.value) return 'PRODUCTION'
  if (props.label) return props.label.toUpperCase()
  if (props.kind === EnvironmentKind.EnvironmentStaging) return 'STAGING'
  if (props.kind === EnvironmentKind.EnvironmentDevelopment) return 'DEV'
  return ''
})

const tone = computed(() => {
  switch (props.kind) {
    case EnvironmentKind.EnvironmentProduction:
      return 'border-warn/60 bg-warn/15 text-warn'
    case EnvironmentKind.EnvironmentStaging:
      return 'border-info/40 bg-info/10 text-info'
    default:
      return 'border-line-strong bg-surface-3 text-ink-muted'
  }
})
</script>

<template>
  <span
    v-if="text"
    class="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] font-bold tracking-widest"
    :class="tone"
  >
    <span v-if="isProduction" aria-hidden="true">⚠</span>
    {{ text }}
  </span>
</template>
