<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { asKind } from '@/composables/kind'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import { useSavedViewsStore } from '@/stores/savedViews'
import { useUIStore } from '@/stores/ui'
import type { SavedView, SavedViewIssueDetail } from '@/types'

const props = defineProps<{
  clusterId: string
  kind: string
  namespace: string
  kindTitle: string
}>()

const clusters = useClusterStore()
const resources = useResourceStore()
const savedViews = useSavedViewsStore()
const ui = useUIStore()

const open = ref(false)
const saving = ref(false)
const title = ref('')
const editingId = ref('')
const selectedColumns = ref<string[]>([])
const unresolved = ref<SavedViewIssueDetail[]>([])

const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)
const resourceKind = computed(() => asKind(props.kind, clusters.catalogues[props.clusterId]))
const views = computed(() => savedViews.byCluster[props.clusterId] ?? [])
const kindViews = computed(() => views.value.filter((view) => view.kind === props.kind))
const columnOptions = computed(() => kindInfo.value?.columns ?? [])

watch(
  () => props.clusterId,
  (clusterId) => {
    if (clusterId) void savedViews.load(clusterId)
  },
  { immediate: true },
)

onMounted(() => {
  if (columnOptions.value.length) {
    selectedColumns.value = columnOptions.value.map((column) => column.key)
  }
})

function issueText(issues: SavedViewIssueDetail[] | null | undefined): string {
  return (issues ?? []).map((issue) => issue.message).join(' ')
}

function beginSave(existing?: SavedView) {
  editingId.value = existing?.id ?? ''
  title.value = existing?.title ?? `${props.kindTitle} view`
  if (existing?.columnIds?.length) {
    selectedColumns.value = [...existing.columnIds]
  } else {
    selectedColumns.value = columnOptions.value.map((column) => column.key)
  }
  open.value = true
}

function toggleColumn(key: string) {
  if (selectedColumns.value.includes(key)) {
    selectedColumns.value = selectedColumns.value.filter((entry) => entry !== key)
    return
  }
  selectedColumns.value = [...selectedColumns.value, key]
}

async function submitSave() {
  const kind = resourceKind.value
  if (!kind || !title.value.trim()) return

  saving.value = true
  try {
    const allSelected =
      selectedColumns.value.length === 0 ||
      selectedColumns.value.length === columnOptions.value.length
    await savedViews.save({
      ...resources.captureSavedView(props.clusterId, kind, props.namespace),
      id: editingId.value || undefined,
      title: title.value.trim(),
      kind,
      columnIds: allSelected ? [] : [...selectedColumns.value],
    })
    open.value = false
    ui.say(editingId.value ? 'Saved view updated.' : 'Saved view created.')
  } catch (err) {
    ui.say(savedViews.errorText(err), 'bad')
  } finally {
    saving.value = false
  }
}

async function openView(view: SavedView) {
  unresolved.value = []
  try {
    const resolution = await savedViews.resolve(props.clusterId, view.id)
    if (!resolution.valid) {
      unresolved.value = resolution.issues ?? []
      ui.say(issueText(resolution.issues), 'bad')
      return
    }
    if (view.namespace && view.namespace !== props.namespace) {
      await clusters.setNamespace(props.clusterId, view.namespace)
    }
    const applied = await resources.applySavedView(resolution.view)
    if (!applied) {
      ui.say('This saved view could not be applied.', 'bad')
      return
    }
    ui.say(`Opened “${view.title}”.`)
  } catch (err) {
    ui.say(savedViews.errorText(err), 'bad')
  }
}

async function removeView(view: SavedView) {
  try {
    await savedViews.remove(props.clusterId, view.id)
    ui.say(`Deleted “${view.title}”.`)
  } catch (err) {
    ui.say(savedViews.errorText(err), 'bad')
  }
}

</script>

<template>
  <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-line px-6 py-2">
    <span class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">Saved views</span>

    <div v-if="kindViews.length" class="flex flex-wrap items-center gap-1.5">
      <button
        v-for="view in kindViews"
        :key="view.id"
        type="button"
        class="rounded-full border border-line bg-surface-3 px-2.5 py-1 text-[11px] text-ink-muted hover:border-line-strong hover:text-ink"
        @click="openView(view)"
      >
        {{ view.title }}
      </button>
    </div>
    <span v-else class="text-[11px] text-ink-faint">No saved views for this resource yet.</span>

    <button
      type="button"
      class="ml-auto rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink"
      @click="beginSave()"
    >
      Save current view…
    </button>

    <p
      v-if="unresolved.length"
      class="w-full rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn"
    >
      {{ issueText(unresolved) }} Edit the saved view or fix the cluster context before applying it.
    </p>
  </div>

  <div
    v-if="open"
    class="fixed inset-0 z-40 flex items-start justify-center bg-black/50 p-6 pt-24"
    @click.self="open = false"
  >
    <form
      class="w-full max-w-md rounded-2xl border border-line bg-surface-2 p-5 shadow-2xl"
      @submit.prevent="submitSave"
    >
      <h2 class="text-sm font-semibold text-ink">
        {{ editingId ? 'Update saved view' : 'Save current view' }}
      </h2>
      <p class="mt-1 text-xs text-ink-faint">
        Saves filters, scope, sort and visible columns for {{ kindTitle }} on this cluster.
      </p>

      <label class="mt-4 block text-xs text-ink-muted">
        Name
        <input
          v-model="title"
          maxlength="80"
          class="mt-1.5 w-full rounded-lg border border-line bg-surface-3 px-3 py-2 text-sm text-ink outline-none focus:border-brand"
          placeholder="Running shop pods"
          required
        />
      </label>

      <fieldset v-if="columnOptions.length" class="mt-4">
        <legend class="text-xs text-ink-muted">Columns</legend>
        <div class="mt-2 flex flex-wrap gap-2">
          <label
            v-for="column in columnOptions"
            :key="column.key"
            class="flex items-center gap-1.5 rounded-full border border-line px-2.5 py-1 text-[11px] text-ink-muted"
          >
            <input
              type="checkbox"
              :checked="selectedColumns.includes(column.key)"
              @change="toggleColumn(column.key)"
            />
            {{ column.title }}
          </label>
        </div>
      </fieldset>

      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
          @click="open = false"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="rounded-lg bg-brand px-3 py-1.5 text-xs font-semibold text-surface-1 disabled:opacity-40"
          :disabled="saving || !title.trim()"
        >
          {{ saving ? 'Saving…' : editingId ? 'Update' : 'Save' }}
        </button>
      </div>

      <ul v-if="kindViews.length" class="mt-4 border-t border-line pt-3 text-xs text-ink-muted">
        <li v-for="view in kindViews" :key="`manage-${view.id}`" class="flex items-center gap-2 py-1">
          <span class="truncate text-ink">{{ view.title }}</span>
          <button type="button" class="ml-auto hover:text-ink" @click="beginSave(view)">Edit</button>
          <button type="button" class="text-bad hover:text-bad/80" @click="removeView(view)">Delete</button>
        </li>
      </ul>
    </form>
  </div>
</template>
