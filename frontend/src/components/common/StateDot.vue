<script setup lang="ts">
import { computed } from 'vue'

import { healthDot, stateDot } from '@/composables/format'
import type { ClusterState, Health } from '@/types'

const props = defineProps<{
  health?: Health
  state?: ClusterState
  /** pulse marks work in progress, so a spinner is not needed beside it. */
  pulse?: boolean
}>()

const colour = computed(() =>
  props.health ? healthDot(props.health) : stateDot(props.state),
)
</script>

<template>
  <span
    class="inline-block size-2 shrink-0 rounded-full"
    :class="[colour, pulse ? 'animate-pulse' : '']"
  />
</template>
