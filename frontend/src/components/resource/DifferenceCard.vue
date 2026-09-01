<script setup lang="ts">
/**
 * One Source vs Live row, compact first.
 *
 * The collapsed view is the finding: omitted → live, who manages it, one
 * confirmed sentence. Range, ignore-rule status and technical evidence sit
 * behind Details, because they are how the finding was reached rather than
 * the finding.
 *
 * Kubernetes decisions stay in Go. This file only lays out fields it was given.
 */
import { computed, ref } from 'vue'

import { DifferenceCause, DifferenceKind, EvidenceKind } from '@/types'
import type { DifferenceEvidence, StateDifference } from '@/types'

const props = defineProps<{
  difference: StateDifference
  argoSync?: string
}>()

const detailsOpen = ref(false)
const evidenceOpen = ref(false)

const explanation = computed(() => props.difference.explanation)
const evidence = computed(() => explanation.value?.evidence ?? [])
const hpa = computed(() => evidence.value.find((item) => item.kind === EvidenceKind.EvidenceHPATarget))
const ignored = computed(() =>
  evidence.value.find((item) => item.kind === EvidenceKind.EvidenceArgoIgnore),
)

const min = computed(() => fact(hpa.value, 'Min'))
const max = computed(() => fact(hpa.value, 'Max'))
const current = computed(() => fact(hpa.value, 'Current'))
const desired = computed(() => fact(hpa.value, 'Desired'))
const scaleRange = computed(() => (min.value && max.value ? `${min.value} – ${max.value}` : ''))

const managedShort = computed(() =>
  (explanation.value?.managedBy ?? '').replace(/^HorizontalPodAutoscaler \//, 'HPA /'),
)

const headline = computed(() => {
  if (desired.value && desired.value === props.difference.live) {
    return `HPA wants ${desired.value} replicas`
  }
  return explanation.value?.summary ?? ''
})

const ignoreLabel = computed(() => {
  switch (explanation.value?.applicationIgnore) {
    case 'applies':
      return ignored.value?.subject || '/spec/replicas'
    case 'absent':
      return 'Not found'
    case 'unread':
      return 'Could not be read'
    default:
      return ''
  }
})

const oneSided = computed(() => {
  if (props.difference.sourceImplicit) return false
  return (
    props.difference.kind === DifferenceKind.DifferenceAddedInLive ||
    props.difference.kind === DifferenceKind.DifferenceMissingInLive
  )
})

const side = computed(() => {
  switch (props.difference.kind) {
    case DifferenceKind.DifferenceAddedInLive:
      return 'Only in the cluster'
    case DifferenceKind.DifferenceMissingInLive:
      return 'Only in the source'
    default:
      return ''
  }
})

const confirmed = computed(
  () => explanation.value?.cause === DifferenceCause.CauseController,
)

function fact(item: DifferenceEvidence | undefined, name: string): string {
  return item?.facts?.find((entry) => entry.name === name)?.value ?? ''
}
</script>

<template>
  <li class="rounded-md bg-surface-2 px-2.5 py-2">
    <p class="text-[11px] font-medium text-ink">
      {{ difference.label || difference.path }}
    </p>
    <p
      v-if="difference.label && difference.subject"
      class="break-all font-mono text-[10px] text-ink-faint"
    >
      {{ difference.subject }}
    </p>

    <p v-if="difference.redacted" class="mt-1.5 text-[11px] text-ink-muted">
      Value differs. This kind's values are not shown here.
    </p>

    <p
      v-else-if="difference.sourceImplicit"
      class="mt-1.5 font-mono text-[11px] text-ink-muted"
    >
      omitted
      <span class="text-ink-faint">· default {{ difference.source }}</span>
      → {{ difference.live }}
    </p>

    <template v-else-if="oneSided">
      <p class="mt-1.5 text-[10px] uppercase tracking-wider text-ink-faint">{{ side }}</p>
      <p class="break-all font-mono text-[11px] text-ink-muted">
        {{ difference.source || difference.live }}
      </p>
    </template>

    <template v-else>
      <p class="mt-1.5 font-mono text-[11px] text-ink-muted">
        {{ difference.source }} → {{ difference.live }}
      </p>
    </template>

    <p v-if="managedShort" class="mt-1.5 text-[11px] text-ink">
      Managed by {{ managedShort }}
    </p>

    <p
      v-if="headline"
      class="mt-1.5 text-[11px] leading-relaxed"
      :class="confirmed ? 'text-ok' : 'text-ink-muted'"
    >
      <span v-if="confirmed" aria-hidden="true">✓ </span>{{ headline }}
    </p>

    <p v-if="argoSync" class="mt-1 text-[11px] text-ink-faint">Argo CD · {{ argoSync }}</p>

    <button
      v-if="explanation"
      class="mt-2 text-[11px] text-ink-faint underline decoration-line underline-offset-2 hover:text-ink-muted"
      @click="detailsOpen = !detailsOpen"
    >
      {{ detailsOpen ? 'Hide details' : 'Details' }}
    </button>

    <div v-if="detailsOpen && explanation" class="mt-2 border-t border-line pt-2">
      <template v-if="!difference.redacted && difference.sourceImplicit">
        <p class="text-[10px] uppercase tracking-wider text-ink-faint">Source</p>
        <p class="font-mono text-[11px] text-ink-muted">omitted</p>
        <p class="text-[10px] text-ink-faint">Kubernetes default {{ difference.source }}</p>
        <p class="mt-1.5 text-[10px] uppercase tracking-wider text-ink-faint">Live</p>
        <p class="font-mono text-[11px] text-ink-muted">{{ difference.live }}</p>
      </template>

      <dl class="mt-2 space-y-1 text-[11px]">
        <div v-if="explanation.managedBy" class="grid grid-cols-[7.5rem_1fr] gap-2">
          <dt class="text-ink-faint">Managed by</dt>
          <dd class="text-ink">{{ explanation.managedBy }}</dd>
        </div>
        <div v-if="scaleRange" class="grid grid-cols-[7.5rem_1fr] gap-2">
          <dt class="text-ink-faint">Range</dt>
          <dd class="font-mono text-ink">{{ scaleRange }} replicas</dd>
        </div>
        <div v-if="current || desired" class="grid grid-cols-[7.5rem_1fr] gap-2">
          <dt class="text-ink-faint">HPA</dt>
          <dd class="font-mono text-ink">
            <template v-if="current">Current {{ current }}</template>
            <template v-if="current && desired"> · </template>
            <template v-if="desired">Desired {{ desired }}</template>
          </dd>
        </div>
        <div v-if="argoSync" class="grid grid-cols-[7.5rem_1fr] gap-2">
          <dt class="text-ink-faint">Argo CD</dt>
          <dd class="text-ink">{{ argoSync }}</dd>
        </div>
        <div v-if="ignoreLabel" class="grid grid-cols-[7.5rem_1fr] gap-2">
          <dt class="text-ink-faint">Application ignore rule</dt>
          <dd class="text-ink">{{ ignoreLabel }}</dd>
        </div>
      </dl>

      <p v-if="explanation.summary" class="mt-2 text-[11px] leading-relaxed text-ink-muted">
        {{ explanation.summary }}
      </p>
      <p v-if="explanation.note" class="mt-1 text-[11px] leading-relaxed text-ink-faint">
        {{ explanation.note }}
      </p>
      <p
        v-if="explanation.applicationIgnore === 'absent'"
        class="mt-1 text-[11px] leading-relaxed text-ink-faint"
      >
        Without an applicable ignore or normalisation rule, Git reconciliation and HPA can contend
        over replicas.
      </p>

      <template v-if="evidence.length || explanation.unchecked?.length">
        <button
          class="mt-2 text-[11px] text-ink-faint underline decoration-line underline-offset-2 hover:text-ink-muted"
          @click="evidenceOpen = !evidenceOpen"
        >
          {{ evidenceOpen ? 'Hide technical evidence' : 'Technical details' }}
        </button>

        <dl v-if="evidenceOpen" class="mt-1.5 space-y-1.5 text-[11px]">
          <div v-for="(item, index) in evidence" :key="index">
            <dt class="text-ink-faint">{{ item.summary }}</dt>
            <dd
              v-for="entry in item.facts ?? []"
              :key="entry.name"
              class="grid grid-cols-[5.5rem_1fr] gap-2 font-mono text-[10px] text-ink-muted"
            >
              <span>{{ entry.name }}</span>
              <span class="break-all">{{ entry.value }}</span>
            </dd>
          </div>
          <p
            v-for="missing in explanation.unchecked ?? []"
            :key="missing"
            class="text-[10px] leading-relaxed text-ink-faint"
          >
            {{ missing }}
          </p>
        </dl>
      </template>
    </div>
  </li>
</template>
