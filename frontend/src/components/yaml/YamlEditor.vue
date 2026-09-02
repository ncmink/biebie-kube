<script setup lang="ts">
/**
 * Editing one live object, with the two questions kept apart.
 *
 *   Original vs Edited   the object as it was when this editor opened,
 *                        against what has been typed since
 *   Source vs Live       what a repository declares, against the cluster
 *
 * The second one lives in the GitOps panel and reads a repository. This
 * component reads nothing but the object it is editing, and the comparison it
 * shows answers exactly one question: what am I about to do?
 *
 * That is why Original is captured once, on open, and never replaced by a
 * watch event or a controller write. An Original that moved would leave the
 * diff describing something nobody did, which is worse than showing no diff.
 * A live change is a separate fact, and it surfaces as the conflict it is
 * before anything is written.
 */
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
// The bare 'monaco-editor' entry point bundles a grammar for every language it
// supports, which is several megabytes for an editor that only ever shows YAML.
// The core API and the one grammar are imported instead.
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import 'monaco-editor/esm/vs/editor/contrib/find/browser/findController'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { api, message } from '@/api'
import { useUIStore } from '@/stores/ui'
import { OwnershipStatus } from '@/types'
import type { Cluster, EditComparison, EditFreshness, MutationGate, ResourceRef } from '@/types'

const props = defineProps<{ clusterId: string; resource: ResourceRef; cluster?: Cluster }>()

const ui = useUIStore()

const host = ref<HTMLElement>()
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor>()
const diffEditor = shallowRef<monaco.editor.IStandaloneDiffEditor>()

/**
 * The snapshot, and the text held against it.
 *
 * `original` is written once per session, by open(). Nothing else assigns to
 * it — not a reload of the live object, not an apply, not a staleness check.
 * Taking a fresh snapshot is a deliberate act with a button on it, and it
 * starts a new session rather than editing this one.
 */
const original = ref('')
const edited = ref('')
const openedVersion = ref('')

const gate = ref<MutationGate>()
const changes = ref<EditComparison>({ dirty: false, equivalent: true, added: 0, removed: 0, hunks: 0 })
const freshness = ref<EditFreshness>()

const view = ref<'edit' | 'changes'>('edit')
const loading = ref(true)
const error = ref('')
const applying = ref(false)
const confirming = ref(false)

// Monaco expects a worker factory; without one it silently falls back to
// running everything on the main thread and freezes on large manifests.
self.MonacoEnvironment = { getWorker: () => new editorWorker() }

monaco.editor.defineTheme('biebie', {
  base: 'vs-dark',
  inherit: true,
  rules: [],
  colors: {
    'editor.background': '#111114',
    'editor.foreground': '#f4f4f5',
    'editorLineNumber.foreground': '#52525b',
    'editorCursor.foreground': '#cbb6e8',
    'editor.selectionBackground': '#cbb6e833',
  },
})

/**
 * Whether a persistent change may be written here at all.
 *
 * Answered by Go, rendered here. Managed means the desired state lives in a
 * repository; unknown means the ownership check could not be completed, which
 * is not the same as nobody owning it. Both close the gate, and the editor
 * opens either way — reading YAML is not a mutation, and a missing Argo CD
 * permission is no reason to take the inspector away.
 */
const writable = computed(() => gate.value?.allowed === true)
const ownership = computed(() => gate.value?.status ?? OwnershipStatus.OwnershipStatusLoading)

const canApply = computed(
  () => writable.value && changes.value.dirty && !changes.value.invalid && !applying.value,
)

const summary = computed(() => {
  if (!changes.value.dirty) return 'No changes yet'
  const places = changes.value.hunks === 1 ? '1 change' : `${changes.value.hunks} changes`
  if (changes.value.equivalent) return `${places} — the same object written differently`
  return `${places}, +${changes.value.added} −${changes.value.removed}`
})

function disposeEditors() {
  editor.value?.dispose()
  editor.value = undefined
  diffEditor.value?.dispose()
  diffEditor.value = undefined
}

/**
 * Both views read and write the same `edited` string, which is what lets the
 * tabs be switched mid-edit without losing anything: the editor is rebuilt,
 * the text is not.
 */
function mountEdit() {
  if (!host.value) return
  disposeEditors()
  const instance = monaco.editor.create(host.value, {
    value: edited.value,
    language: 'yaml',
    theme: 'biebie',
    readOnly: !writable.value,
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 12,
    scrollBeyondLastLine: false,
    tabSize: 2,
  })
  instance.onDidChangeModelContent(() => void typed(instance.getValue()))
  editor.value = instance
}

function mountChanges() {
  if (!host.value) return
  disposeEditors()
  const instance = monaco.editor.createDiffEditor(host.value, {
    theme: 'biebie',
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 12,
    renderSideBySide: true,
    scrollBeyondLastLine: false,
    // The left side is the snapshot and is never editable. The right side is
    // the same document the Edit tab holds, so typing in the diff is typing in
    // the editor rather than into a copy that has to be reconciled later.
    originalEditable: false,
    readOnly: !writable.value,
  })
  instance.setModel({
    original: monaco.editor.createModel(original.value, 'yaml'),
    modified: monaco.editor.createModel(edited.value, 'yaml'),
  })
  instance.getModifiedEditor().onDidChangeModelContent(() => {
    void typed(instance.getModifiedEditor().getValue())
  })
  diffEditor.value = instance
}

function render() {
  if (view.value === 'changes') mountChanges()
  else mountEdit()
}

/**
 * Dirty is decided in Go, on both the text and the parsed object.
 *
 * Doing it here would mean reimplementing "is this the same object written
 * differently?" in TypeScript, and getting a reordered manifest wrong in a way
 * that quietly claims an apply would change something when it would not.
 */
let pending = 0
async function typed(text: string) {
  edited.value = text
  const ticket = ++pending
  try {
    const result = await api.compareEdit(original.value, text)
    // A stale answer arriving after a newer keystroke would make the change
    // count flicker backwards.
    if (ticket === pending) changes.value = result
  } catch {
    // Ignored on purpose. The comparison is a review aid; failing to count
    // hunks must not interrupt somebody who is typing. The text-level
    // fallback keeps the Apply button honest in the meantime.
    if (ticket === pending) changes.value = { ...changes.value, dirty: text !== original.value }
  }
}

/** open captures the snapshot this session is measured against. */
async function open() {
  loading.value = true
  error.value = ''
  freshness.value = undefined
  view.value = 'edit'
  try {
    const session = await api.openEditor(props.clusterId, props.resource)
    original.value = session.original
    edited.value = session.original
    openedVersion.value = session.resourceVersion
    gate.value = session.gate
    changes.value = { dirty: false, equivalent: true, added: 0, removed: 0, hunks: 0 }
    render()
  } catch (err) {
    error.value = message(err)
  } finally {
    loading.value = false
  }
}

/**
 * Revert restores the snapshot and touches nothing else.
 *
 * It is local by construction: no binding is called, so there is nothing here
 * that could reach a cluster. What it undoes is typing, which is the only
 * thing that has happened.
 */
function revertAll() {
  edited.value = original.value
  changes.value = { dirty: false, equivalent: true, added: 0, removed: 0, hunks: 0 }
  render()
}

async function attemptApply() {
  error.value = ''
  freshness.value = undefined
  try {
    const state = await api.editorFreshness(props.clusterId, props.resource, openedVersion.value)
    if (state.stale || state.gone || state.unchecked) {
      // Shown rather than resolved. Rebasing somebody's edits onto an object
      // that changed underneath them is a decision only they can make, and
      // Go would refuse the write anyway.
      freshness.value = state
      return
    }
  } catch (err) {
    freshness.value = { stale: false, gone: false, unchecked: message(err) }
    return
  }
  confirming.value = true
}

async function apply() {
  confirming.value = false
  applying.value = true
  error.value = ''
  try {
    const result = await api.applyYAML(props.clusterId, props.resource, edited.value, openedVersion.value)
    ui.say(result.changed ? `Applied to ${props.resource.name}.` : 'No change: the cluster already matched.')
    // A successful write makes this session's snapshot history. The new one is
    // the object as it now stands, which is what the next edit is measured
    // against.
    await open()
  } catch (err) {
    error.value = message(err)
  } finally {
    applying.value = false
  }
}

/** refreshOriginal starts a new session against the object as it stands now. */
async function refreshOriginal() {
  freshness.value = undefined
  await open()
}

watch(view, render)
watch(writable, render)

onMounted(open)
onBeforeUnmount(disposeEditors)
watch(() => [props.resource.kind, props.resource.name, props.resource.namespace], open)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-3 border-b border-line px-6 py-2">
      <div class="flex rounded-lg border border-line p-0.5">
        <button
          v-for="tab in (['edit', 'changes'] as const)"
          :key="tab"
          class="rounded-md px-2.5 py-1 text-xs capitalize"
          :class="view === tab ? 'bg-surface-3 text-ink' : 'text-ink-muted hover:text-ink'"
          @click="view = tab"
        >
          {{ tab }}
          <span v-if="tab === 'changes' && changes.hunks" class="ml-1 text-brand">{{ changes.hunks }}</span>
        </button>
      </div>

      <span class="text-xs" :class="changes.dirty ? 'text-ink-muted' : 'text-ink-faint'">
        {{ summary }}
      </span>

      <div class="ml-auto flex gap-2">
        <button
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-40"
          :disabled="!changes.dirty"
          title="Restore the manifest as it was when this editor opened. Nothing is sent to the cluster."
          @click="revertAll"
        >
          Revert all
        </button>
        <button
          class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 disabled:opacity-40"
          :disabled="!canApply"
          @click="attemptApply"
        >
          Review &amp; apply
        </button>
      </div>
    </div>

    <!--
      Why a write is unavailable, said before it is wanted. Loading and unknown
      are spelled out separately: "still checking" and "could not check" call
      for different things from the person reading them, and neither is
      "unmanaged".
    -->
    <p
      v-if="!loading && !writable"
      class="shrink-0 border-b border-line px-6 py-2 text-xs"
      :class="ownership === OwnershipStatus.OwnershipStatusManaged ? 'bg-surface-2 text-ink-muted' : 'bg-warn/10 text-warn'"
    >
      <template v-if="ownership === OwnershipStatus.OwnershipStatusLoading">
        Checking GitOps ownership… editing is read-only until this is known.
      </template>
      <template v-else>{{ gate?.reason }}</template>
    </p>

    <p v-if="changes.invalid" class="shrink-0 border-b border-line bg-warn/10 px-6 py-2 text-xs text-warn">
      This is not valid YAML yet, so it cannot be applied: {{ changes.invalid }}
    </p>

    <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-6 py-2 text-xs text-bad">
      {{ error }}
    </p>

    <!-- The conflict, offered as a choice rather than resolved by guessing. -->
    <div v-if="freshness" class="shrink-0 border-b border-line bg-warn/10 px-6 py-2.5">
      <p class="text-xs text-warn">{{ freshness.reason || freshness.unchecked }}</p>
      <div class="mt-2 flex gap-2">
        <button
          v-if="!freshness.gone"
          class="rounded-lg border border-warn/40 px-2.5 py-1 text-xs text-warn hover:bg-warn/10"
          @click="refreshOriginal"
        >
          Refresh original
        </button>
        <button
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
          @click="freshness = undefined"
        >
          Cancel
        </button>
      </div>
      <p v-if="!freshness.gone" class="mt-2 text-[11px] text-ink-faint">
        Refreshing replaces the original with the object as it stands now. Your edits are not merged into it —
        reapply the ones you still want.
      </p>
    </div>

    <p v-if="loading" class="px-6 py-3 text-xs text-ink-muted">Reading manifest…</p>

    <div ref="host" class="min-h-0 flex-1" />

    <ConfirmDialog
      :open="confirming"
      :title="`Apply changes to ${resource.name}?`"
      detail="The live resource will be updated at the version this editor opened at. If it changes before the write lands, the cluster rejects it rather than overwriting."
      :cluster="cluster"
      confirm-label="Apply"
      :require-typing="cluster?.environmentKind === 'production' ? resource.name : undefined"
      @cancel="confirming = false"
      @confirm="apply"
    />
  </div>
</template>
