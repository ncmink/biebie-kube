<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ContextMenu from '@/components/common/ContextMenu.vue'
import CreateResourceDialog from '@/components/resource/CreateResourceDialog.vue'
import ResourceActionDialog from '@/components/resource/ResourceActionDialog.vue'
import ResourceDrawer from '@/components/resource/ResourceDrawer.vue'
import ResourceTable from '@/components/resource/ResourceTable.vue'
import { api, message } from '@/api'
import { actionsFor, menuItems } from '@/composables/actions'
import type { ActionDescriptor } from '@/composables/actions'
import { asKind, singularTitle } from '@/composables/kind'
import { applyQuerySuggestion, suggestQueryTokens } from '@/composables/querySuggest'
import type { ContextMenuItem } from '@/composables/menu'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind, AccessMode, QueryMode } from '@/types'
import type { ResourceRow } from '@/types'

const props = defineProps<{ clusterId: string; kind: string }>()

const clusters = useClusterStore()
const resources = useResourceStore()
const ui = useUIStore()

const namespace = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')
const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)
const cluster = computed(() => clusters.clusters.find((entry) => entry.id === props.clusterId))
const readOnly = computed(
  () => clusters.policies[props.clusterId]?.effectiveMode === AccessMode.AccessModeReadOnly,
)
const resourceKind = computed(() => asKind(props.kind, clusters.catalogues[props.clusterId]))
const heading = computed(() => singularTitle(kindInfo.value?.title ?? props.kind))
const selected = ref<ResourceRow | null>(null)
const inspectRevision = ref(0)

const identity = computed(() => `${props.clusterId}/${props.kind}/${namespace.value}`)

/**
 * count is what the table is actually showing, against what exists.
 *
 * Reporting the loaded row count alone is how a list of twelve thousand pods
 * came to look like a list of two thousand: the number on screen matched the
 * rows on screen, and both were wrong about the cluster.
 */
const count = computed(() => {
  const shown = resources.rows.length
  const { matched, total, syncing } = resources
  const suffix = syncing ? '+' : ''
  const filtering =
    resources.queryMode === QueryMode.QueryModeExpression
      ? resources.expression.trim() !== ''
      : resources.filter.trim() !== ''

  if (filtering) return `${matched}${suffix} of ${total}${suffix} match`
  if (shown < matched) return `${shown} of ${matched}${suffix}`
  return `${matched}${suffix}`
})

/**
 * The menu belongs to the page rather than to the table, because the drawer
 * opens the same one for the row it is showing. Two menus built from two
 * copies of the same list is how a row's actions and the inspector's come to
 * disagree about what a kind offers.
 */
const menu = ref<{ row: ResourceRow; x: number; y: number } | null>(null)
const acting = ref<{ row: ResourceRow; action: ActionDescriptor } | null>(null)
const deleting = ref<ResourceRow | null>(null)

/**
 * Creating is offered from the list rather than from a kind, and the dialog
 * asks the backend whether it is allowed rather than being hidden here.
 *
 * Whether a namespace is somebody's GitOps destination is a question with a
 * cluster in the answer, and asking it on every list render would be a round
 * trip per navigation for a button most people will not press.
 */
const creating = ref(false)

const expressionInput = ref<HTMLInputElement>()
const expressionCaret = ref(0)
const suggestionIndex = ref(0)
const suggestionsOpen = ref(true)

const expressionSuggestions = computed(() =>
  suggestionsOpen.value ? suggestQueryTokens(resources.expression, expressionCaret.value) : [],
)

function rememberCaret(target: HTMLInputElement) {
  expressionCaret.value = target.selectionStart ?? target.value.length
}

function onExpressionInput(event: Event) {
  const target = event.target as HTMLInputElement
  rememberCaret(target)
  resources.setExpression(target.value)
  suggestionsOpen.value = true
  suggestionIndex.value = 0
}

function pickSuggestion(suggestion: string) {
  const { next, caret } = applyQuerySuggestion(
    resources.expression,
    expressionCaret.value,
    suggestion,
  )
  resources.setExpression(next)
  expressionCaret.value = caret
  suggestionIndex.value = 0
  void nextTick(() => {
    const input = expressionInput.value
    if (!input) return
    input.focus()
    input.setSelectionRange(caret, caret)
  })
}

function onExpressionKeydown(event: KeyboardEvent) {
  const options = expressionSuggestions.value
  if (!options.length) return

  if (event.key === 'ArrowDown') {
    event.preventDefault()
    suggestionIndex.value = (suggestionIndex.value + 1) % options.length
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    suggestionIndex.value = (suggestionIndex.value - 1 + options.length) % options.length
    return
  }
  if (event.key === 'Enter' || event.key === 'Tab') {
    event.preventDefault()
    pickSuggestion(options[suggestionIndex.value] ?? options[0])
    return
  }
  if (event.key === 'Escape') {
    suggestionsOpen.value = false
  }
}

async function reload() {
  await resources.load(props.clusterId, props.kind, namespace.value)
  const current = selected.value
  if (!current) return
  const next = resources.rows.find((row) => row.key === current.key)
  if (next) selected.value = next
}

function refresh() {
  inspectRevision.value++
  void reload()
}

onMounted(() => {
  void reload()
})
watch(identity, () => {
  selected.value = null
  menu.value = null
  resources.reset()
  void reload()
})

function open(row: ResourceRow) {
  selected.value = row
}

const offered = computed(() => (menu.value ? actionsFor(kindInfo.value, menu.value.row) : []))

const items = computed<ContextMenuItem[]>(() => [
  ...menuItems(offered.value),
  { id: 'delete', label: 'Delete…', danger: true, divider: offered.value.length > 0 },
])

function openMenu(row: ResourceRow, event: MouseEvent) {
  menu.value = { row, x: event.clientX, y: event.clientY }
}

function choose(id: string) {
  // Both are read before the menu closes: the list of actions is derived from
  // the row the menu was opened on, and clearing it first would leave nothing
  // to look the chosen action up in.
  const opened = menu.value
  const actions = offered.value
  menu.value = null
  if (!opened) return

  if (id === 'delete') {
    deleting.value = opened.row
    return
  }
  const action = actions.find((entry) => entry.action === id)
  if (action) acting.value = { row: opened.row, action }
}

async function remove() {
  const row = deleting.value
  deleting.value = null
  if (!row || !resourceKind.value) return

  try {
    await api.deleteResource(props.clusterId, {
      kind: resourceKind.value,
      namespace: row.namespace,
      name: row.name,
    })
    ui.say(`Deleted ${row.name}.`)
    if (selected.value?.key === row.key) selected.value = null
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header class="flex shrink-0 items-center gap-3 border-b border-line px-6 py-3">
      <h1 class="text-sm font-semibold text-ink">{{ kindInfo?.title ?? kind }}</h1>
      <span class="font-mono text-xs text-ink-faint">{{ count }}</span>
      <span v-if="resources.syncing" class="text-xs text-ink-faint">syncing…</span>
      <div class="ml-auto flex items-center gap-2">
        <div class="flex rounded-lg border border-line p-0.5 text-[11px]">
          <button
            class="rounded-md px-2 py-1"
            :class="resources.queryMode === QueryMode.QueryModeText ? 'bg-surface-3 text-ink' : 'text-ink-muted hover:text-ink'"
            @click="resources.setQueryMode(QueryMode.QueryModeText)"
          >
            Name
          </button>
          <button
            class="rounded-md px-2 py-1"
            :class="resources.queryMode === QueryMode.QueryModeExpression ? 'bg-surface-3 text-ink' : 'text-ink-muted hover:text-ink'"
            @click="resources.setQueryMode(QueryMode.QueryModeExpression)"
          >
            Expression
          </button>
        </div>
        <input
          v-if="resources.queryMode === QueryMode.QueryModeText"
          :value="resources.filter"
          class="w-64 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
          placeholder="Filter by name"
          spellcheck="false"
          @input="resources.setFilter(($event.target as HTMLInputElement).value)"
        />
        <div v-else class="relative">
          <input
            ref="expressionInput"
            :value="resources.expression"
            class="w-[28rem] rounded-lg border border-line bg-surface-2 px-3 py-1.5 font-mono text-sm text-ink outline-none focus:border-brand"
            placeholder="restarts >= 5 && status = CrashLoopBackOff"
            spellcheck="false"
            autocomplete="off"
            @input="onExpressionInput"
            @keydown="onExpressionKeydown"
            @click="rememberCaret(($event.target as HTMLInputElement))"
            @keyup="rememberCaret(($event.target as HTMLInputElement))"
            @blur="suggestionsOpen = false"
          />
          <ul
            v-if="expressionSuggestions.length"
            class="absolute z-20 mt-1 max-h-48 w-full overflow-auto rounded-lg border border-line bg-surface-2 py-1 shadow-lg"
          >
            <li v-for="(item, index) in expressionSuggestions" :key="item">
              <button
                type="button"
                class="block w-full px-3 py-1.5 text-left font-mono text-xs"
                :class="index === suggestionIndex ? 'bg-surface-3 text-ink' : 'text-ink-muted hover:bg-surface-3 hover:text-ink'"
                @mousedown.prevent="pickSuggestion(item)"
              >
                {{ item }}
              </button>
            </li>
          </ul>
        </div>
      </div>
      <button
        class="rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="refresh"
      >
        Refresh
      </button>
      <button
        class="rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink disabled:opacity-40"
        :disabled="readOnly"
        :title="readOnly ? 'This cluster is read-only.' : undefined"
        @click="creating = true"
      >
        Create {{ heading }}…
      </button>
    </header>

    <p
      v-if="resources.queryError"
      class="shrink-0 border-b border-line bg-warn/10 px-6 py-2 text-xs text-warn"
    >
      {{ resources.queryError }} — table still uses the last valid query.
    </p>

    <p
      v-if="kindInfo?.sensitive"
      class="shrink-0 border-b border-line bg-warn/10 px-6 py-2 text-xs text-warn"
    >
      Secret values are shown as stored base64. The eye decodes and shows plain text.
    </p>

    <div class="flex min-h-0 flex-1">
      <div class="min-w-0 flex-1">
        <p v-if="resources.error" class="m-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
          {{ resources.error }}
        </p>
        <p v-else-if="resources.loading" class="px-6 py-6 text-sm text-ink-muted">Loading…</p>
        <ResourceTable
          v-else
          :identity="identity"
          :rows="resources.rows"
          :columns="resources.columns"
          :namespaced="resources.namespaced"
          :sort-key="resources.sortKey"
          :sort-desc="resources.sortDesc"
          :selected="selected"
          @open="open"
          @menu="openMenu"
          @sort="resources.sortBy"
          @end="resources.more"
        />
      </div>
      <ResourceDrawer
        v-if="selected"
        :cluster-id="clusterId"
        :kind="kind"
        :row="selected"
        :kind-title="kindInfo?.title ?? kind"
        :revision="inspectRevision"
        @menu="openMenu(selected, $event)"
        @delete="deleting = selected"
        @close="selected = null"
      />
    </div>

    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="items"
      @select="choose"
      @close="menu = null"
    />

    <ResourceActionDialog
      v-if="acting && resourceKind"
      :cluster-id="clusterId"
      :kind="resourceKind"
      :kind-title="kindInfo?.title ?? kind"
      :row="acting.row"
      :action="acting.action"
      :cluster="cluster"
      :read-only="readOnly"
      @close="acting = null"
    />

    <CreateResourceDialog
      v-if="creating"
      :cluster-id="clusterId"
      :kind="kind"
      :kind-title="kindInfo?.title ?? kind"
      :namespace="namespace"
      :cluster="cluster"
      @created="refresh"
      @close="creating = false"
    />

    <ConfirmDialog
      :open="!!deleting"
      :title="`Delete ${heading} “${deleting?.name}”?`"
      :detail="
        deleting?.namespace
          ? `In namespace ${deleting.namespace}. This cannot be undone.`
          : 'This cannot be undone.'
      "
      :cluster="cluster"
      :require-typing="
        cluster?.environmentKind === EnvironmentKind.EnvironmentProduction
          ? deleting?.name
          : undefined
      "
      @cancel="deleting = null"
      @confirm="remove"
    />
  </div>
</template>
