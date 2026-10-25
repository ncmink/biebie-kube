<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { api, message } from '@/api'
import type { IncidentReport, ResourceRef } from '@/types'

const props = defineProps<{ clusterId: string; resource: ResourceRef }>()
const emit = defineEmits<{ 'ask-ai': [prompt: string] }>()

const report = ref<IncidentReport | null>(null)
const loading = ref(false)
const error = ref('')

let token = 0

const coverageLabel = computed(() => {
  const state = report.value?.coverage.state
  switch (state) {
    case 'complete':
      return 'Complete'
    case 'partial':
      return 'Partial'
    default:
      return 'Unknown'
  }
})

async function load() {
  const mine = ++token
  loading.value = true
  error.value = ''
  try {
    const next = await api.explainResource(props.clusterId, props.resource)
    if (mine !== token) return
    report.value = next
  } catch (err) {
    if (mine !== token) return
    error.value = message(err)
    report.value = null
  } finally {
    if (mine === token) loading.value = false
  }
}

watch(
  () => [props.clusterId, props.resource.kind, props.resource.namespace, props.resource.name],
  load,
  { immediate: true },
)
</script>

<template>
  <section>
    <div class="mb-2 flex items-center justify-between gap-2">
      <h2 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Explain</h2>
      <div class="flex items-center gap-1.5">
        <button
          type="button"
          class="ask-ai-btn inline-flex items-center gap-1 rounded-lg bg-brand px-2.5 py-1 text-[11px] font-semibold text-surface-1 transition hover:bg-brand-strong"
          @click="emit('ask-ai', `Why is ${resource.name} unhealthy, and what should I check next?`)"
        >
          <svg viewBox="0 0 24 24" class="ask-ai-sparkle-icon size-3" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true">
            <path d="m12 3 1.2 3.6L17 8l-3.8 1.2L12 13l-1.2-3.8L7 8l3.8-1.4Z" />
            <path d="M5 14l.8 2.2L8 17l-2.2.8L5 20l-.8-2.2L2 17l2.2-.8Z" />
          </svg>
          <span>Ask AI</span>
        </button>
        <button
          class="rounded-lg border border-line px-2 py-1 text-[11px] text-ink-muted hover:text-ink disabled:opacity-40"
          :disabled="loading"
          @click="load"
        >
          Refresh
        </button>
      </div>
    </div>

    <p v-if="error" class="rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-xs text-bad">
      {{ error }}
    </p>
    <p v-else-if="loading && !report" class="text-xs text-ink-muted">Collecting evidence…</p>

    <template v-else-if="report">
      <p class="text-xs text-ink-muted">
        Coverage: <span class="font-medium text-ink">{{ coverageLabel }}</span>
        <span v-if="report.coverage.missing?.length">
          · missing {{ report.coverage.missing.join(', ') }}
        </span>
      </p>

      <div v-if="!report.findings?.length" class="mt-3 rounded-lg border border-line bg-surface-2 px-3 py-2 text-xs text-ink-muted">
        No supported findings from the evidence collected. This does not mean the object is healthy —
        only that nothing in the current rules explains it yet.
      </div>

      <article
        v-for="finding in report.findings ?? []"
        :key="finding.ruleId"
        class="mt-3 rounded-lg border border-line bg-surface-2 px-3 py-3"
      >
        <div class="flex items-start justify-between gap-2">
          <h3 class="text-sm font-semibold text-ink">{{ finding.summary }}</h3>
          <span
            class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
            :class="
              finding.severity === 'critical'
                ? 'bg-bad/15 text-bad'
                : finding.severity === 'warning'
                  ? 'bg-warn/15 text-warn'
                  : 'bg-surface-3 text-ink-muted'
            "
          >
            {{ finding.confidenceClass }}
          </span>
        </div>
        <p class="mt-2 text-xs leading-relaxed text-ink-muted">{{ finding.explanation }}</p>

        <button
          type="button"
          class="ask-ai-btn mt-2.5 inline-flex items-center gap-1 rounded-md border border-brand/40 bg-brand/15 px-2 py-1 text-[11px] font-semibold text-brand transition hover:bg-brand hover:text-surface-1"
          @click="
            emit(
              'ask-ai',
              `Explain this finding for ${resource.name}: ${finding.summary}. ${finding.explanation}`,
            )
          "
        >
          <svg viewBox="0 0 24 24" class="ask-ai-sparkle-icon size-3" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true">
            <path d="m12 3 1.2 3.6L17 8l-3.8 1.2L12 13l-1.2-3.8L7 8l3.8-1.4Z" />
          </svg>
          <span>Ask AI about this finding</span>
        </button>

        <ul v-if="finding.observedFacts?.length" class="mt-3 space-y-1 text-xs text-ink">
          <li v-for="fact in finding.observedFacts" :key="fact">✓ {{ fact }}</li>
        </ul>

        <div v-if="finding.nextSteps?.length" class="mt-3">
          <p class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">
            What to inspect next
          </p>
          <ul class="mt-1 space-y-1 text-xs text-brand">
            <li v-for="step in finding.nextSteps" :key="step">→ {{ step }}</li>
          </ul>
        </div>
      </article>

      <ul v-if="report.issues?.length" class="mt-3 space-y-1 text-xs text-warn">
        <li v-for="issue in report.issues" :key="`${issue.scope}-${issue.code}`">
          {{ issue.scope }}: {{ issue.message }}
        </li>
      </ul>
    </template>
  </section>
</template>
