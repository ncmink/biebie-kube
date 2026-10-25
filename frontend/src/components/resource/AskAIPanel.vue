<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { api, copyToClipboard, message } from '@/api'
import { parseSafeMarkdown } from '@/composables/safeMarkdown'
import { useAIStore } from '@/stores/ai'
import { useUIStore } from '@/stores/ui'
import type { AIContextPreview, ResourceRef } from '@/types'

const props = defineProps<{
  clusterId: string
  resource: ResourceRef
  initialPrompt?: string
}>()

const ai = useAIStore()
const ui = useUIStore()

const prompt = ref(props.initialPrompt ?? '')
const cliId = ref('')
const showConsent = ref(false)
const pendingAsk = ref(false)

const quickPrompts = [
  'Summarize this resource',
  'Why is this unhealthy?',
  'What should I check next?',
]

const availableClis = computed(() => ai.clis.filter((c) => c.available))
const selectedCli = computed(() => ai.clis.find((c) => c.id === cliId.value))
const blocks = computed(() => parseSafeMarkdown(ai.answer))

watch(
  () => props.initialPrompt,
  (value) => {
    if (value) prompt.value = value
  },
)

// The preview is built by the same Go path as the real request, with the
// question left as a placeholder, so it reads the cluster once per resource
// rather than once per keystroke.
const preview = ref<AIContextPreview | null>(null)
const previewLoading = ref(false)
const previewError = ref('')
const previewOpen = ref(true)
const fullPromptOpen = ref(false)
let previewToken = 0

const includedSections = computed(() => preview.value?.sections?.filter((s) => s.included) ?? [])
const skippedSections = computed(() => preview.value?.sections?.filter((s) => !s.included) ?? [])

function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}

async function loadPreview() {
  const mine = ++previewToken
  previewLoading.value = true
  previewError.value = ''
  try {
    const next = await api.previewAskAI({
      clusterId: props.clusterId,
      ref: props.resource,
      prompt: '',
      cliId: '',
    })
    if (mine !== previewToken) return
    preview.value = next
  } catch (err) {
    if (mine !== previewToken) return
    preview.value = null
    previewError.value = message(err)
  } finally {
    if (mine === previewToken) previewLoading.value = false
  }
}

watch(
  () => [props.clusterId, props.resource.kind, props.resource.namespace, props.resource.name],
  () => {
    fullPromptOpen.value = false
    void loadPreview()
  },
  { immediate: true },
)

onMounted(async () => {
  await ai.refreshCLIs()
  const def = ai.settings.defaultCli
  if (def && availableClis.value.some((c) => c.id === def)) {
    cliId.value = def
  } else if (availableClis.value[0]) {
    cliId.value = availableClis.value[0].id
  }
})

async function runAsk() {
  const text = prompt.value.trim()
  if (!text) {
    ui.say('Type a question first.', 'bad')
    return
  }
  if (!cliId.value) {
    ui.say('No AI CLI is available. Configure one in Settings → AI.', 'bad')
    return
  }
  const cli = selectedCli.value
  if (cli && !cli.acknowledged) {
    showConsent.value = true
    pendingAsk.value = true
    return
  }
  try {
    await ai.ask(props.clusterId, props.resource, text, cliId.value)
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function confirmConsent() {
  showConsent.value = false
  if (!cliId.value) return
  try {
    await ai.acknowledge(cliId.value)
    if (pendingAsk.value) {
      pendingAsk.value = false
      await ai.ask(props.clusterId, props.resource, prompt.value.trim(), cliId.value)
    }
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function copyAnswer() {
  if (!ai.answer) return
  const ok = await copyToClipboard(ai.answer)
  ui.say(ok ? 'Copied.' : 'Could not copy.', ok ? 'ok' : 'bad')
}
</script>

<template>
  <section class="flex h-full min-h-0 flex-col gap-3">
    <div class="rounded-lg border border-line bg-surface-2">
      <div class="flex items-center justify-between gap-2 px-3 py-2">
        <button
          type="button"
          class="flex min-w-0 items-center gap-1.5 text-left text-[11px] font-semibold uppercase tracking-wider text-ink-faint hover:text-ink"
          :aria-expanded="previewOpen"
          @click="previewOpen = !previewOpen"
        >
          <span>{{ previewOpen ? '▾' : '▸' }}</span>
          <span>Data that will be sent</span>
          <span v-if="preview" class="font-normal normal-case tracking-normal text-ink-muted">
            · {{ size(preview.totalBytes) }}{{ preview.truncated ? ' (truncated)' : '' }}
          </span>
        </button>
        <button
          type="button"
          class="shrink-0 text-[11px] text-ink-muted hover:text-ink disabled:opacity-40"
          :disabled="previewLoading"
          @click="loadPreview"
        >
          Refresh
        </button>
      </div>

      <div v-if="previewOpen" class="border-t border-line px-3 py-2 text-xs">
        <p v-if="previewError" class="text-bad">{{ previewError }}</p>
        <p v-else-if="previewLoading && !preview" class="text-ink-muted">Collecting context…</p>

        <template v-else-if="preview">
          <ul class="space-y-1">
            <li
              v-for="section in includedSections"
              :key="section.title"
              class="flex items-baseline justify-between gap-3"
            >
              <span class="min-w-0">
                <span class="text-ok">✓</span>
                <span class="ml-1 text-ink">{{ section.title }}</span>
                <span v-if="section.detail" class="ml-1 text-ink-muted">— {{ section.detail }}</span>
              </span>
              <span class="shrink-0 font-mono text-[11px] text-ink-faint">{{ size(section.bytes) }}</span>
            </li>
            <li v-for="section in skippedSections" :key="section.title" class="text-ink-faint">
              <span>–</span>
              <span class="ml-1">{{ section.title }}</span>
              <span v-if="section.detail"> — {{ section.detail }}</span>
            </li>
          </ul>

          <div class="mt-2">
            <p class="text-[10px] font-semibold uppercase tracking-wider text-ink-faint">Removed before sending</p>
            <ul v-if="preview.scrubbed?.length" class="mt-1 space-y-0.5 text-ink-muted">
              <li v-for="note in preview.scrubbed" :key="note">✕ {{ note }}</li>
            </ul>
            <p v-else class="mt-1 text-ink-muted">Nothing sensitive was found to remove.</p>
          </div>

          <p class="mt-2 text-ink-faint">
            Never sent: kubeconfig, tokens, other resources' manifests, or cluster-wide data. The CLI may forward
            this to its AI provider.
          </p>

          <button
            type="button"
            class="mt-2 text-[11px] text-brand hover:underline"
            @click="fullPromptOpen = !fullPromptOpen"
          >
            {{ fullPromptOpen ? 'Hide full message' : 'Show full message' }}
          </button>
          <pre
            v-if="fullPromptOpen"
            class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded bg-surface-3 p-2 font-mono text-[11px] text-ink"
          >{{ preview.prompt }}</pre>
        </template>
      </div>
    </div>

    <div class="flex flex-wrap gap-2">
      <button
        v-for="q in quickPrompts"
        :key="q"
        type="button"
        class="rounded-lg border border-line px-2 py-1 text-[11px] text-ink-muted hover:text-ink"
        @click="prompt = q"
      >
        {{ q }}
      </button>
    </div>

    <label class="block text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Question</label>
    <textarea
      v-model="prompt"
      rows="3"
      class="w-full resize-y rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-xs text-ink"
      placeholder="Ask about this resource…"
    />

    <div class="flex flex-wrap items-center gap-2">
      <label class="text-[11px] text-ink-muted">CLI</label>
      <select
        v-model="cliId"
        class="min-w-[12rem] rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink"
      >
        <option v-for="c in availableClis" :key="c.id" :value="c.id">
          {{ c.name }}{{ c.version ? ` (${c.version})` : '' }}
        </option>
      </select>
      <button
        type="button"
        class="rounded-lg bg-brand px-3 py-1.5 text-xs font-semibold text-surface-1 disabled:opacity-40"
        :disabled="ai.streaming || !availableClis.length"
        @click="runAsk"
      >
        {{ ai.streaming ? 'Running…' : 'Ask' }}
      </button>
      <button
        v-if="ai.streaming"
        type="button"
        class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="ai.cancel()"
      >
        Cancel
      </button>
      <button
        v-if="ai.answer"
        type="button"
        class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="copyAnswer"
      >
        Copy
      </button>
    </div>

    <p v-if="!availableClis.length && !ai.loadingClis" class="text-xs text-warn">
      No AI CLI was detected. Install Cursor Agent (<code class="font-mono">agent</code>), Claude Code, Codex, or Gemini CLI, or add a custom command in Settings → AI.
    </p>

    <div
      v-if="showConsent"
      class="rounded-lg border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-ink"
    >
      <p>
        Data from this resource will be sent to
        <strong>{{ selectedCli?.name }}</strong>, which may send it to its provider. Continue?
      </p>
      <p v-if="preview" class="mt-1 text-ink-muted">
        {{ includedSections.map((s) => s.title).join(', ') }} · {{ size(preview.totalBytes) }}
      </p>
      <div class="mt-2 flex gap-2">
        <button type="button" class="rounded-lg bg-brand px-2 py-1 text-[11px] font-semibold text-surface-1" @click="confirmConsent">
          I understand
        </button>
        <button type="button" class="rounded-lg border border-line px-2 py-1 text-[11px]" @click="showConsent = false">
          Cancel
        </button>
      </div>
    </div>

    <p v-if="ai.streamError" class="rounded-lg border border-bad/40 bg-bad/10 px-3 py-2 text-xs text-bad">
      {{ ai.streamError }}
    </p>

    <div v-if="ai.answer || ai.streaming" class="min-h-0 flex-1 overflow-y-auto rounded-lg border border-line bg-surface-2 px-3 py-3">
      <p v-if="ai.streaming && !ai.answer" class="text-xs text-ink-muted">Waiting for output…</p>
      <template v-for="(block, idx) in blocks" :key="idx">
        <h3
          v-if="block.kind === 'heading'"
          class="mt-2 font-semibold text-ink"
          :class="block.level === 1 ? 'text-sm' : 'text-xs'"
        >
          {{ block.text }}
        </h3>
        <pre
          v-else-if="block.kind === 'code'"
          class="mt-2 overflow-x-auto rounded bg-surface-3 p-2 font-mono text-[11px] text-ink"
        >{{ block.text }}</pre>
        <ul v-else-if="block.kind === 'list'" class="mt-2 list-disc space-y-1 pl-4 text-xs text-ink-muted">
          <li v-for="item in block.items" :key="item">{{ item }}</li>
        </ul>
        <p v-else class="mt-2 text-xs leading-relaxed text-ink-muted">{{ block.text }}</p>
      </template>
    </div>
  </section>
</template>
