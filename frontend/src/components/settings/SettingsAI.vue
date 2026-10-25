<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { message } from '@/api'
import { useAIStore } from '@/stores/ai'
import { useUIStore } from '@/stores/ui'
import type { AICliInfo, AICustomCommand } from '@/types'

const ai = useAIStore()
const ui = useUIStore()

const defaultCli = ref('')
const customLabel = ref('')
const customPath = ref('')
const customArgs = ref('')
const promptPlaceholder = '{{PROMPT}}'

const clis = computed(() => ai.clis)
const builtInClis = computed(() => clis.value.filter((c) => !c.custom))

const statusLine = ref<{ tone: 'ok' | 'bad'; text: string } | null>(null)
let statusTimer: number | undefined

function showStatus(text: string, tone: 'ok' | 'bad' = 'ok') {
  statusLine.value = { tone, text }
  window.clearTimeout(statusTimer)
  statusTimer = window.setTimeout(() => {
    statusLine.value = null
  }, 5000)
}

function formatArg(arg: string): string {
  if (arg === '') return '""'
  if (/\s/.test(arg)) return JSON.stringify(arg)
  return arg
}

function invocationLine(c: AICliInfo): string {
  const bin = c.path || c.command || c.id
  const args = (c.runArgs ?? []).map(formatArg)
  if (c.custom) {
    const hasPrompt = (c.runArgs ?? []).some((a) => a.includes(promptPlaceholder))
    const suffix = hasPrompt ? '  < stdin (prompt)' : ` ${promptPlaceholder}`
    return [bin, ...args].join(' ') + suffix
  }
  return [bin, ...args].join(' ') + '  < stdin (prompt)'
}

function fillFromPreset(c: AICliInfo) {
  customLabel.value = `${c.name} (custom)`
  customPath.value = c.path || `/path/to/${c.command || c.id}`
  const args = (c.runArgs ?? []).map(formatArg)
  customArgs.value = [...args, promptPlaceholder].join(' ')
  showStatus(`Filled custom command from ${c.name} defaults. Adjust path or flags, then click Add custom CLI.`)
}

onMounted(async () => {
  await ai.refreshCLIs()
  defaultCli.value = ai.settings.defaultCli ?? ''
})

async function saveDefault() {
  try {
    await ai.saveSettings({ ...ai.settings, defaultCli: defaultCli.value })
    showStatus('Default CLI saved.')
    ui.say('AI settings saved.', 'ok')
  } catch (err) {
    showStatus(message(err), 'bad')
    ui.say(message(err), 'bad')
  }
}

function parseCustomArgs(raw: string): string[] {
  const tokens = raw.match(/"[^"]*"|'[^']*'|\S+/g) ?? []
  return tokens.map((t) => {
    if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) {
      return t.slice(1, -1)
    }
    return t
  })
}

async function addCustom() {
  const path = customPath.value.trim()
  const label = customLabel.value.trim() || 'Custom CLI'
  const args = parseCustomArgs(customArgs.value)
  if (!path) {
    ui.say('Custom command needs an absolute path.', 'bad')
    return
  }
  const entry: AICustomCommand = {
    id: `custom_${Date.now()}`,
    label,
    path,
    args,
  }
  try {
    await ai.saveSettings({
      ...ai.settings,
      custom: [...(ai.settings.custom ?? []), entry],
    })
    customLabel.value = ''
    customPath.value = ''
    customArgs.value = ''
    showStatus(`Added "${label}".`)
    ui.say('Custom command added.', 'ok')
  } catch (err) {
    showStatus(message(err), 'bad')
    ui.say(message(err), 'bad')
  }
}

async function removeCustom(id: string) {
  try {
    await ai.saveSettings({
      ...ai.settings,
      custom: (ai.settings.custom ?? []).filter((c) => c.id !== id),
    })
    showStatus('Custom command removed.')
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function resetAcknowledgements() {
  try {
    await ai.saveSettings({ ...ai.settings, acknowledged: [] })
    showStatus('Privacy acknowledgements cleared. Ask AI will ask again before sending data to each CLI.')
    ui.say('Acknowledgements cleared.', 'ok')
  } catch (err) {
    showStatus(message(err), 'bad')
    ui.say(message(err), 'bad')
  }
}

async function recheck() {
  try {
    await ai.refreshCLIs()
    defaultCli.value = ai.settings.defaultCli ?? ''
    const available = clis.value.filter((c) => c.available).length
    showStatus(`Refreshed ${clis.value.length} CLI(s); ${available} available on PATH.`)
    ui.say('CLIs rechecked.', 'ok')
  } catch (err) {
    showStatus(message(err), 'bad')
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <div class="space-y-4 text-sm">
    <p class="text-xs text-ink-muted">
      Ask AI runs a CLI installed on your machine. BiebieKube does not call a cloud AI service.
    </p>

    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-50"
        :disabled="ai.loadingClis"
        @click="recheck"
      >
        {{ ai.loadingClis ? 'Checking…' : 'Recheck CLIs' }}
      </button>
      <button
        type="button"
        class="rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-50"
        :disabled="ai.loadingClis"
        @click="resetAcknowledgements"
      >
        Reset privacy acknowledgements
      </button>
    </div>

    <p
      v-if="statusLine"
      class="rounded-lg border px-3 py-2 text-xs"
      :class="
        statusLine.tone === 'bad'
          ? 'border-bad/40 bg-bad/10 text-bad'
          : 'border-ok/40 bg-ok/10 text-ink'
      "
      role="status"
    >
      {{ statusLine.text }}
    </p>

    <ul class="space-y-2 text-xs">
      <li
        v-for="c in clis"
        :key="c.id"
        class="rounded-lg border border-line bg-surface-2 px-3 py-2.5"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="flex items-center gap-2">
            <span class="font-medium text-ink">{{ c.name }}</span>
            <span v-if="c.version" class="font-mono text-[11px] text-ink-faint">{{ c.version }}</span>
          </div>
          <div class="flex flex-wrap items-center gap-1.5">
            <button
              v-if="!c.custom"
              type="button"
              class="rounded border border-line px-1.5 py-0.5 text-[10px] text-ink-muted hover:border-brand/50 hover:text-ink"
              title="Copy these default flags into the Custom command form below"
              @click="fillFromPreset(c)"
            >
              Customize…
            </button>
            <span
              v-if="c.acknowledged"
              class="rounded px-1.5 py-0.5 text-[10px] font-medium text-ink-muted"
              title="You confirmed sending resource data to this CLI"
            >
              Privacy OK
            </span>
            <span
              class="rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase"
              :class="c.available ? 'bg-ok/15 text-ok' : 'bg-bad/15 text-bad'"
            >
              {{ c.available ? 'Available' : 'Missing' }}
            </span>
          </div>
        </div>

        <div class="mt-1.5 rounded bg-surface-3 px-2 py-1 font-mono text-[11px] text-ink-muted">
          <span class="text-ink-faint">$ </span>{{ invocationLine(c) }}
        </div>

        <p v-if="c.reason" class="mt-1 text-ink-muted">{{ c.reason }}</p>
      </li>
    </ul>

    <div>
      <label class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Default CLI</label>
      <div class="mt-1 flex flex-wrap items-center gap-2">
        <select
          v-model="defaultCli"
          class="min-w-[14rem] rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs"
        >
          <option value="">—</option>
          <option v-for="c in clis.filter((x) => x.available)" :key="c.id" :value="c.id">
            {{ c.name }}
          </option>
        </select>
        <button type="button" class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 hover:bg-brand-strong" @click="saveDefault">
          Save
        </button>
      </div>
    </div>

    <div class="rounded-lg border border-line bg-surface-2/60 p-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Custom command</h3>
        <div class="flex flex-wrap items-center gap-1 text-[11px]">
          <span class="text-ink-faint">Fill preset:</span>
          <button
            v-for="b in builtInClis"
            :key="b.id"
            type="button"
            class="rounded border border-line bg-surface-2 px-1.5 py-0.5 text-[10px] text-ink-muted hover:border-brand/50 hover:text-ink"
            @click="fillFromPreset(b)"
          >
            {{ b.name }}
          </button>
        </div>
      </div>

      <p class="mt-1 text-xs leading-relaxed text-ink-muted">
        Use this when a CLI is not on PATH or when you want to override flags (for example, choosing a specific
        <code class="font-mono">--model</code>). Include <code class="font-mono">{{ promptPlaceholder }}</code> to
        send the scrubbed resource context via <strong>stdin</strong>; if omitted, the prompt is appended as the final argument.
      </p>

      <div class="mt-2.5 grid gap-2 sm:grid-cols-2">
        <input
          v-model="customLabel"
          placeholder="Label (e.g. Cursor Sonnet)"
          class="rounded-lg border border-line bg-surface-2 px-2.5 py-1.5 text-xs text-ink"
        />
        <input
          v-model="customPath"
          placeholder="/Users/you/.local/bin/agent"
          class="rounded-lg border border-line bg-surface-2 px-2.5 py-1.5 font-mono text-xs text-ink"
        />
        <input
          v-model="customArgs"
          :placeholder="`--print --trust --mode ask --output-format text ${promptPlaceholder}`"
          class="sm:col-span-2 rounded-lg border border-line bg-surface-2 px-2.5 py-1.5 font-mono text-xs text-ink"
        />
      </div>
      <button
        type="button"
        class="mt-2 rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 hover:bg-brand-strong"
        @click="addCustom"
      >
        Add custom CLI
      </button>

      <ul v-if="ai.settings.custom?.length" class="mt-3 space-y-1.5 border-t border-line pt-2.5 text-xs">
        <li
          v-for="c in ai.settings.custom"
          :key="c.id"
          class="flex flex-wrap items-center justify-between gap-2 rounded bg-surface-3 px-2.5 py-1.5"
        >
          <div class="min-w-0">
            <span class="font-medium text-ink">{{ c.label }}</span>
            <p class="truncate font-mono text-[11px] text-ink-faint">
              {{ c.path }} {{ (c.args ?? []).map(formatArg).join(' ') }}
            </p>
          </div>
          <button type="button" class="shrink-0 text-bad hover:underline" @click="removeCustom(c.id)">Remove</button>
        </li>
      </ul>
    </div>
  </div>
</template>
