<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { openInBrowser } from '@/api'
import MenuSelect from '@/components/common/MenuSelect.vue'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import { Kind } from '@/types'

const props = defineProps<{
  clusterId: string
  kind: string
  namespace: string
  custom?: boolean
}>()

type PropertyPreset = {
  label: string
  field: string
  placeholder: string
  values?: string[]
}

const podProperties: PropertyPreset[] = [
  {
    label: 'Status',
    field: 'status.phase',
    placeholder: 'Choose a status',
    values: ['Pending', 'Running', 'Succeeded', 'Failed', 'Unknown'],
  },
  { label: 'Node', field: 'spec.nodeName', placeholder: 'worker-01' },
  { label: 'Name', field: 'metadata.name', placeholder: 'api-server' },
  {
    label: 'Service account',
    field: 'spec.serviceAccountName',
    placeholder: 'default',
  },
  { label: 'Pod IP', field: 'status.podIP', placeholder: '10.42.0.12' },
]

const labelOperatorOptions = [
  { value: '=', label: 'is' },
  { value: '!=', label: 'is not' },
  { value: 'in', label: 'is one of' },
]

const propertyOperatorOptions = [
  { value: '=', label: 'is' },
  { value: '!=', label: 'is not' },
]

const clusters = useClusterStore()
const resources = useResourceStore()

const open = ref(false)
const applying = ref(false)
const labelKey = ref('')
const labelOperator = ref('=')
const labelValue = ref('')
const propertyField = ref('status.phase')
const propertyOperator = ref('=')
const propertyValue = ref('')

const propertyPresets = computed(() =>
  props.kind === Kind.KindPod ? podProperties : [],
)
const selectedProperty = computed(() =>
  propertyPresets.value.find((preset) => preset.field === propertyField.value),
)
const namespaceOptions = computed(() => [
  { value: '', label: 'All namespaces' },
  ...(clusters.namespaces[props.clusterId] ?? []).map((namespace) => ({
    value: namespace,
    label: namespace,
  })),
])
const propertyOptions = computed(() =>
  propertyPresets.value.map((preset) => ({ value: preset.field, label: preset.label })),
)
const propertyValueOptions = computed(() =>
  (selectedProperty.value?.values ?? []).map((value) => ({ value, label: value })),
)

const valueSuggestions = computed(() => {
  if (propertyField.value === 'spec.nodeName') {
    return unique(resources.rows.map((row) => row.fields?.node ?? ''))
  }
  if (propertyField.value === 'metadata.name') {
    return unique(resources.rows.map((row) => row.name))
  }
  return []
})

const suggestionListId = computed(
  () => `resource-filter-values-${props.clusterId.replace(/[^a-zA-Z0-9_-]/g, '-')}`,
)

const labelTerms = computed(() => splitSelector(resources.labelSelector))
const fieldTerms = computed(() => splitSelector(resources.fieldSelector))
const hasFilters = computed(
  () => Boolean(props.namespace || labelTerms.value.length || fieldTerms.value.length),
)

const canAddLabel = computed(() => Boolean(labelKey.value.trim() && labelValue.value.trim()))
const canAddProperty = computed(() =>
  Boolean(propertyField.value && propertyValue.value.trim()),
)

watch(propertyPresets, (presets) => {
  if (presets.some((preset) => preset.field === propertyField.value)) return
  propertyField.value = presets[0]?.field ?? ''
})

watch(propertyField, () => {
  propertyValue.value = ''
})

function unique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))].sort((left, right) => left.localeCompare(right))
}

/** Splits comma-separated requirements without splitting a set such as in (prod, staging). */
function splitSelector(selector: string): string[] {
  const terms: string[] = []
  let start = 0
  let depth = 0
  let escaped = false

  for (let index = 0; index < selector.length; index++) {
    const character = selector[index]
    if (escaped) {
      escaped = false
      continue
    }
    if (character === '\\') {
      escaped = true
      continue
    }
    if (character === '(') depth++
    if (character === ')' && depth > 0) depth--
    if (character !== ',' || depth !== 0) continue

    const term = selector.slice(start, index).trim()
    if (term) terms.push(term)
    start = index + 1
  }

  const tail = selector.slice(start).trim()
  if (tail) terms.push(tail)
  return terms
}

function labelDescription(term: string): string {
  const set = term.match(/^(.+?)\s+(in|notin)\s+\((.*)\)$/)
  if (set) {
    const [, key, operator, values] = set
    return operator === 'in'
      ? `${key.trim()}: ${values}`
      : `${key.trim()} is not one of ${values}`
  }
  const unequal = term.match(/^([^!=]+)!=(.*)$/)
  if (unequal) return `${unequal[1].trim()} is not ${unequal[2].trim()}`
  const equal = term.match(/^([^=]+)==?(.*)$/)
  if (equal) return `${equal[1].trim()}: ${equal[2].trim()}`
  return term
}

function propertyDescription(term: string): string {
  const match = term.match(/^([^!=]+)(!?==?)(.*)$/)
  if (!match) return `Resource properties: ${term}`

  const [, field, operator, value] = match
  const title = podProperties.find((preset) => preset.field === field.trim())?.label ?? field.trim()
  return operator.startsWith('!')
    ? `${title} is not ${value.trim()}`
    : `${title}: ${value.trim()}`
}

function labelRequirement(): string {
  const key = labelKey.value.trim()
  const value = labelValue.value.trim()
  if (labelOperator.value === 'in') {
    const values = value
      .split(',')
      .map((entry) => entry.trim())
      .filter(Boolean)
      .join(',')
    return `${key} in (${values})`
  }
  return `${key}${labelOperator.value}${value}`
}

async function replaceSelectors(labels: string[], fields: string[]): Promise<boolean> {
  resources.draftLabelSelector = labels.join(',')
  resources.draftFieldSelector = fields.join(',')
  applying.value = true
  try {
    return await resources.applySelectors()
  } finally {
    applying.value = false
  }
}

async function addLabel() {
  if (!canAddLabel.value) return
  const added = await replaceSelectors([...labelTerms.value, labelRequirement()], fieldTerms.value)
  if (!added) return
  labelKey.value = ''
  labelValue.value = ''
}

async function addProperty() {
  if (!canAddProperty.value) return
  const requirement = `${propertyField.value}${propertyOperator.value}${propertyValue.value.trim()}`
  const added = await replaceSelectors(labelTerms.value, [...fieldTerms.value, requirement])
  if (added) propertyValue.value = ''
}

async function removeLabel(index: number) {
  await replaceSelectors(
    labelTerms.value.filter((_, current) => current !== index),
    fieldTerms.value,
  )
}

async function removeProperty(index: number) {
  await replaceSelectors(
    labelTerms.value,
    fieldTerms.value.filter((_, current) => current !== index),
  )
}

async function applyAdvanced() {
  applying.value = true
  try {
    await resources.applySelectors()
  } finally {
    applying.value = false
  }
}

async function chooseNamespace(value: string) {
  await clusters.setNamespace(props.clusterId, value)
}

async function clearAll() {
  const cleared = await replaceSelectors([], [])
  if (cleared && props.namespace) await clusters.setNamespace(props.clusterId, '')
}
</script>

<template>
  <section class="shrink-0 border-b border-line px-6 py-2.5">
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-xs font-semibold text-ink-muted hover:border-line-strong hover:text-ink"
        :aria-expanded="open"
        @click="open = !open"
      >
        Filter resources
        <span class="text-base font-normal leading-none text-brand" aria-hidden="true">
          {{ open ? '−' : '+' }}
        </span>
      </button>

      <template v-if="hasFilters">
        <span class="ml-1 text-[10px] font-semibold uppercase tracking-wider text-ink-faint">
          Filtering by:
        </span>
        <button
          v-if="namespace"
          type="button"
          class="rounded-full border border-line bg-surface-3 px-2.5 py-1 text-[11px] text-ink-muted hover:border-line-strong hover:text-ink"
          title="Clear namespace filter"
          @click="clusters.setNamespace(clusterId, '')"
        >
          Namespace: <span class="text-ink">{{ namespace }}</span> ×
        </button>
        <button
          v-for="(term, index) in labelTerms"
          :key="`label-${index}-${term}`"
          type="button"
          class="rounded-full border border-line bg-surface-3 px-2.5 py-1 text-[11px] text-ink-muted hover:border-line-strong hover:text-ink"
          :title="`Kubernetes label selector: ${term}`"
          @click="removeLabel(index)"
        >
          {{ labelDescription(term) }} ×
        </button>
        <button
          v-for="(term, index) in fieldTerms"
          :key="`field-${index}-${term}`"
          type="button"
          class="rounded-full border border-line bg-surface-3 px-2.5 py-1 text-[11px] text-ink-muted hover:border-line-strong hover:text-ink"
          :title="`Kubernetes field selector: ${term}`"
          @click="removeProperty(index)"
        >
          {{ propertyDescription(term) }} ×
        </button>
        <button
          type="button"
          class="px-1.5 py-1 text-[11px] text-ink-faint hover:text-ink"
          :disabled="applying"
          @click="clearAll"
        >
          × Clear all
        </button>
      </template>
    </div>

    <div v-if="open" class="mt-3 rounded-xl border border-line bg-surface-2 p-4">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h2 class="text-sm font-semibold text-ink">Filter resources</h2>
          <p class="mt-0.5 text-xs text-ink-faint">
            Choose what you want to see. Biebie will use the appropriate Kubernetes filter.
          </p>
        </div>
        <button
          type="button"
          class="text-xs text-ink-faint hover:text-ink"
          aria-label="Close resource filters"
          @click="open = false"
        >
          Close
        </button>
      </div>

      <div class="mt-4 grid gap-4 xl:grid-cols-2">
        <div>
          <label class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">
            Namespace
          </label>
          <MenuSelect
            class="mt-1.5"
            :model-value="namespace"
            :options="namespaceOptions"
            label="Namespace"
            searchable
            search-placeholder="Filter namespaces…"
            empty-label="No namespace matches."
            @update:model-value="chooseNamespace"
          />
        </div>

        <form @submit.prevent="addLabel">
          <label class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">
            Labels
          </label>
          <div class="mt-1.5 grid grid-cols-[minmax(7rem,1fr)_auto_minmax(8rem,1fr)_auto] gap-2">
            <input
              v-model="labelKey"
              class="min-w-0 rounded-lg border border-line bg-surface-3 px-3 py-2 font-mono text-xs text-ink outline-none focus:border-brand"
              placeholder="app"
              aria-label="Label key"
              spellcheck="false"
              autocomplete="off"
            />
            <MenuSelect
              v-model="labelOperator"
              class="min-w-[5.5rem]"
              :options="labelOperatorOptions"
              label="Label operator"
            />
            <input
              v-model="labelValue"
              class="min-w-0 rounded-lg border border-line bg-surface-3 px-3 py-2 font-mono text-xs text-ink outline-none focus:border-brand"
              :placeholder="labelOperator === 'in' ? 'prod, staging' : 'shop'"
              aria-label="Label value"
              spellcheck="false"
              autocomplete="off"
            />
            <button
              type="submit"
              class="rounded-lg bg-brand px-3 py-2 text-xs font-semibold text-surface-1 disabled:cursor-not-allowed disabled:opacity-40"
              :disabled="!canAddLabel || applying"
            >
              Add
            </button>
          </div>
        </form>
      </div>

      <form v-if="propertyPresets.length" class="mt-4" @submit.prevent="addProperty">
        <label class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">
          Resource properties
        </label>
        <div class="mt-1.5 grid max-w-3xl grid-cols-[minmax(10rem,1fr)_auto_minmax(10rem,1fr)_auto] gap-2">
          <MenuSelect
            v-model="propertyField"
            :options="propertyOptions"
            label="Resource property"
          />
          <MenuSelect
            v-model="propertyOperator"
            class="min-w-[5.5rem]"
            :options="propertyOperatorOptions"
            label="Property operator"
          />
          <MenuSelect
            v-if="selectedProperty?.values"
            v-model="propertyValue"
            :options="propertyValueOptions"
            :placeholder="selectedProperty.placeholder"
            label="Property value"
          />
          <input
            v-else
            v-model="propertyValue"
            class="min-w-0 rounded-lg border border-line bg-surface-3 px-3 py-2 font-mono text-xs text-ink outline-none focus:border-brand"
            :placeholder="selectedProperty?.placeholder"
            :list="valueSuggestions.length ? suggestionListId : undefined"
            aria-label="Property value"
            spellcheck="false"
            autocomplete="off"
          />
          <datalist :id="suggestionListId">
            <option v-for="value in valueSuggestions" :key="value" :value="value" />
          </datalist>
          <button
            type="submit"
            class="rounded-lg bg-brand px-3 py-2 text-xs font-semibold text-surface-1 disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="!canAddProperty || applying"
          >
            Add
          </button>
        </div>
        <p class="mt-1.5 text-[11px] text-ink-faint">
          Pod properties shown here are supported by Kubernetes server-side filtering.
        </p>
      </form>

      <p v-else class="mt-4 rounded-lg border border-line bg-surface-3 px-3 py-2 text-xs text-ink-muted">
        <template v-if="custom">
          Property filters are not advertised for custom resources. Use Advanced only when
          this CRD documents a supported field selector.
        </template>
        <template v-else>
          No verified property filters are available for this resource. Labels are safe to
          use; Advanced field selectors may be rejected by the cluster.
        </template>
      </p>

      <p
        v-if="resources.selectorError"
        class="mt-4 rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn"
      >
        {{ resources.selectorError }}
      </p>

      <details class="mt-4 border-t border-line pt-3">
        <summary class="cursor-pointer text-xs font-semibold text-ink-muted hover:text-ink">
          Advanced Kubernetes selectors
        </summary>
        <p class="mt-2 text-xs text-ink-faint">
          Enter raw selector syntax only when you know the fields this resource supports.
          Unsupported field selectors are never applied as table filters automatically.
        </p>
        <div class="mt-3 grid gap-3 xl:grid-cols-2">
          <label class="text-xs text-ink-muted">
            Labels
            <input
              :value="resources.draftLabelSelector"
              class="mt-1.5 w-full rounded-lg border border-line bg-surface-3 px-3 py-2 font-mono text-xs text-ink outline-none focus:border-brand"
              placeholder="app=shop,environment in (prod,staging)"
              spellcheck="false"
              autocomplete="off"
              @input="resources.draftLabelSelector = ($event.target as HTMLInputElement).value"
              @keydown.enter.prevent="applyAdvanced"
            />
          </label>
          <label class="text-xs text-ink-muted">
            Resource properties
            <input
              :value="resources.draftFieldSelector"
              class="mt-1.5 w-full rounded-lg border border-line bg-surface-3 px-3 py-2 font-mono text-xs text-ink outline-none focus:border-brand"
              placeholder="status.phase=Running"
              spellcheck="false"
              autocomplete="off"
              @input="resources.draftFieldSelector = ($event.target as HTMLInputElement).value"
              @keydown.enter.prevent="applyAdvanced"
            />
          </label>
        </div>
        <div class="mt-3 flex items-center gap-3">
          <button
            type="button"
            class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:border-line-strong hover:text-ink disabled:opacity-40"
            :disabled="applying"
            @click="applyAdvanced"
          >
            {{ applying ? 'Applying…' : 'Apply filters' }}
          </button>
          <button
            type="button"
            class="text-xs text-brand hover:text-brand-strong"
            @click="openInBrowser('https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/')"
          >
            What is this?
          </button>
        </div>
      </details>
    </div>
  </section>
</template>
