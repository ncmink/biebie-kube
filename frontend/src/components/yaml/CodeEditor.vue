<script setup lang="ts">
/**
 * A Monaco editor for one document, in one of two languages.
 *
 * Separate from YamlEditor.vue, which owns a live resource's snapshot, its
 * comparison against what has been typed, and the guarded write back. This one
 * owns nothing: it renders text and reports edits, so both the
 * authoring surfaces and the read-only preview of what cdk8s generated can use
 * the same component without inheriting a resource lifecycle none of them has.
 *
 * Only two grammars are imported. The bare 'monaco-editor' entry point bundles
 * one for every language it supports, which is several megabytes for an editor
 * that will only ever show Kubernetes YAML and a cdk8s file.
 */
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import 'monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution'
import 'monaco-editor/esm/vs/editor/contrib/find/browser/findController'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'

const props = withDefaults(
  defineProps<{ modelValue: string; language: 'yaml' | 'typescript'; readonly?: boolean }>(),
  { readonly: false },
)

const emit = defineEmits<{ 'update:modelValue': [string] }>()

const host = ref<HTMLElement>()
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor>()

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

function mount() {
  if (!host.value) return
  editor.value?.dispose()
  editor.value = monaco.editor.create(host.value, {
    value: props.modelValue,
    language: props.language,
    theme: 'biebie',
    readOnly: props.readonly,
    automaticLayout: true,
    minimap: { enabled: false },
    fontSize: 12,
    scrollBeyondLastLine: false,
    tabSize: 2,
  })
  editor.value.onDidChangeModelContent(() => {
    emit('update:modelValue', editor.value?.getValue() ?? '')
  })
}

// A value that arrived from outside — a starter template, a manifest cdk8s
// generated — is written into the model only when it differs from what is
// already there. Setting it unconditionally would move the cursor to the top
// of the file on every keystroke.
watch(
  () => props.modelValue,
  (value) => {
    if (editor.value && editor.value.getValue() !== value) editor.value.setValue(value)
  },
)

watch(() => props.language, mount)

onMounted(mount)
onBeforeUnmount(() => editor.value?.dispose())
</script>

<template>
  <div ref="host" class="h-full min-h-0 w-full" />
</template>
