<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
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
import type { Cluster, ResourceRef } from '@/types'

const props = defineProps<{ clusterId: string; resource: ResourceRef; cluster?: Cluster }>()

const ui = useUIStore()

const host = ref<HTMLElement>()
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor | monaco.editor.IStandaloneDiffEditor>()
const original = ref('')
const loading = ref(true)
const error = ref('')
const comparing = ref(false)
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

function currentText(): string {
  const instance = editor.value
  if (!instance) return ''
  if ('getModifiedEditor' in instance) return instance.getModifiedEditor().getValue()
  return instance.getValue()
}

function dispose() {
  editor.value?.dispose()
  editor.value = undefined
}

function mountPlain(value: string) {
  if (!host.value) return
  dispose()
  editor.value = monaco.editor.create(host.value, {
    value,
    language: 'yaml',
    theme: 'biebie',
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 12,
    scrollBeyondLastLine: false,
    tabSize: 2,
  })
}

function mountDiff(before: string, after: string) {
  if (!host.value) return
  dispose()
  const diff = monaco.editor.createDiffEditor(host.value, {
    theme: 'biebie',
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 12,
    renderSideBySide: true,
    scrollBeyondLastLine: false,
  })
  diff.setModel({
    original: monaco.editor.createModel(before, 'yaml'),
    modified: monaco.editor.createModel(after, 'yaml'),
  })
  editor.value = diff
}

async function load() {
  loading.value = true
  error.value = ''
  comparing.value = false
  try {
    original.value = await api.getYAML(props.clusterId, props.resource)
    mountPlain(original.value)
  } catch (err) {
    error.value = message(err)
  } finally {
    loading.value = false
  }
}

/**
 * Comparing is a required step, not a convenience.
 *
 * The difference is computed against a freshly read object, so an edit written
 * while a controller was changing the same resource shows the conflict rather
 * than quietly winning.
 */
async function compare() {
  const edited = currentText()
  try {
    const diff = await api.diffYAML(props.clusterId, props.resource, edited)
    original.value = diff.current
    mountDiff(diff.current, diff.edited)
    comparing.value = true
  } catch (err) {
    error.value = message(err)
  }
}

async function apply() {
  confirming.value = false
  applying.value = true
  error.value = ''
  try {
    const result = await api.applyYAML(props.clusterId, props.resource, currentText())
    ui.say(result.changed ? `Applied to ${props.resource.name}.` : 'No change: the cluster already matched.')
    await load()
  } catch (err) {
    error.value = message(err)
  } finally {
    applying.value = false
  }
}

onMounted(load)
onBeforeUnmount(dispose)
watch(() => [props.resource.kind, props.resource.name, props.resource.namespace], load)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b border-line px-6 py-2">
      <span class="text-xs text-ink-faint">
        {{ comparing ? 'Comparing your edit with the live resource' : 'Editing a copy — nothing is sent until you apply' }}
      </span>
      <div class="ml-auto flex gap-2">
        <button
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
          @click="load"
        >
          Reload
        </button>
        <button
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
          @click="compare"
        >
          Compare
        </button>
        <button
          class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 disabled:opacity-40"
          :disabled="applying"
          @click="confirming = true"
        >
          Apply
        </button>
      </div>
    </div>

    <p v-if="error" class="shrink-0 border-b border-line bg-bad/10 px-6 py-2 text-xs text-bad">
      {{ error }}
    </p>
    <p v-if="loading" class="px-6 py-3 text-xs text-ink-muted">Reading manifest…</p>

    <div ref="host" class="min-h-0 flex-1" />

    <ConfirmDialog
      :open="confirming"
      :title="`Apply changes to ${resource.name}?`"
      detail="The live resource will be updated. If it changed since you loaded it, the cluster will reject the write rather than overwrite."
      :cluster="cluster"
      confirm-label="Apply"
      :require-typing="cluster?.environmentKind === 'production' ? resource.name : undefined"
      @cancel="confirming = false"
      @confirm="apply"
    />
  </div>
</template>
