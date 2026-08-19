<script setup lang="ts">
import { computed } from 'vue'

import { Layer, ProbeResult } from '@/types'
import type { Diagnosis } from '@/types'

const props = defineProps<{ diagnosis: Diagnosis }>()

const layerNames: Partial<Record<Layer, string>> = {
  [Layer.LayerAccess]: 'Access',
  [Layer.LayerNetwork]: 'Network',
  [Layer.LayerTCP]: 'TCP',
  [Layer.LayerTLS]: 'TLS',
  [Layer.LayerKubernetes]: 'Kubernetes',
}

const marks: Partial<Record<ProbeResult, string>> = {
  [ProbeResult.ProbePassed]: '✓',
  [ProbeResult.ProbeFailed]: '✕',
  [ProbeResult.ProbeSkipped]: '·',
}

const tones: Partial<Record<ProbeResult, string>> = {
  [ProbeResult.ProbePassed]: 'text-ok',
  [ProbeResult.ProbeFailed]: 'text-bad',
  [ProbeResult.ProbeSkipped]: 'text-ink-faint',
}

const probes = computed(() => props.diagnosis.probes ?? [])
</script>

<template>
  <div class="rounded-xl border border-line bg-surface-2 p-4">
    <p class="text-sm font-semibold text-ink">{{ diagnosis.summary }}</p>
    <p v-if="diagnosis.detail" class="mt-1 text-xs leading-relaxed text-ink-muted">
      {{ diagnosis.detail }}
    </p>

    <dl v-if="probes.length" class="mt-4 space-y-1.5">
      <div v-for="probe in probes" :key="probe.layer" class="flex items-start gap-3 text-xs">
        <dt class="w-24 shrink-0 text-ink-faint">{{ layerNames[probe.layer] ?? probe.layer }}</dt>
        <dd class="flex min-w-0 flex-1 items-start gap-2">
          <span class="font-mono" :class="tones[probe.result]" aria-hidden="true">
            {{ marks[probe.result] }}
          </span>
          <span
            class="min-w-0 flex-1"
            :class="probe.result === ProbeResult.ProbeSkipped ? 'text-ink-faint' : 'text-ink-muted'"
          >
            {{ probe.result === ProbeResult.ProbeSkipped ? 'Not tested' : probe.detail }}
          </span>
          <span v-if="probe.elapsedMs" class="shrink-0 font-mono text-ink-faint">
            {{ probe.elapsedMs }}ms
          </span>
        </dd>
      </div>
    </dl>

    <slot />
  </div>
</template>
