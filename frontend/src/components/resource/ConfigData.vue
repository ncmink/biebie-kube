<script setup lang="ts">
import { ref, watch } from 'vue'

import { copyToClipboard } from '@/api'
import { useUIStore } from '@/stores/ui'
import type { DataEntry } from '@/types'

const props = defineProps<{ entries: DataEntry[]; sensitive?: boolean }>()

const ui = useUIStore()
const revealed = ref(new Set<string>())
const copied = ref('')

watch(
  () => props.entries,
  () => {
    revealed.value = new Set()
    copied.value = ''
  },
)

function isOpen(key: string): boolean {
  return revealed.value.has(key)
}

function toggle(key: string) {
  const next = new Set(revealed.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  revealed.value = next
  if (copied.value === key) copied.value = ''
}

/**
 * decodeSecret turns Kubernetes' stored base64 into UTF-8.
 *
 * The inspector sends Secret data as stored: still encoded. Decoding is the
 * eye click, not the payload. Bytes that are not UTF-8 stay encoded so a
 * TLS key is not replaced with replacement characters.
 */
function decodeSecret(value: string): string | null {
  if (value === '') return ''
  try {
    const binary = atob(value)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    return null
  }
}

function isBinary(entry: DataEntry): boolean {
  if (entry.binary) return true
  if (!props.sensitive) return false
  return decodeSecret(entry.value) == null
}

function displayValue(entry: DataEntry): string {
  if (!props.sensitive || entry.binary) return entry.value
  if (!isOpen(entry.key)) return entry.value
  return decodeSecret(entry.value) ?? entry.value
}

function lineCount(value: string): number {
  return Math.min(8, Math.max(1, value.split('\n').length))
}

async function copyValue(entry: DataEntry, event: Event) {
  const input = event.currentTarget as HTMLTextAreaElement
  input.select()
  if (!(await copyToClipboard(displayValue(entry)))) {
    ui.say('Could not copy to the clipboard.', 'bad')
    return
  }
  copied.value = entry.key
  ui.say(`Copied ${entry.key}.`)
}
</script>

<template>
  <section>
    <h2 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Data</h2>

    <p v-if="!entries.length" class="mt-3 text-xs text-ink-faint">No data keys.</p>

    <ul v-else class="mt-3 space-y-4">
      <li v-for="entry in entries" :key="entry.key">
        <div class="flex items-center gap-2">
          <p class="text-sm font-medium text-ink">{{ entry.key }}</p>
          <span
            v-if="isBinary(entry)"
            class="text-[10px] uppercase tracking-wider text-ink-faint"
          >
            binary
          </span>
          <button
            v-if="sensitive && !isBinary(entry)"
            class="text-ink-faint hover:text-ink"
            :aria-label="isOpen(entry.key) ? `Hide ${entry.key}` : `Reveal ${entry.key}`"
            :title="
              isOpen(entry.key)
                ? 'Show the stored base64.'
                : 'Decode and show the plain text value.'
            "
            @click="toggle(entry.key)"
          >
            <svg v-if="isOpen(entry.key)" viewBox="0 0 24 24" class="size-4" fill="none" aria-hidden="true">
              <path
                d="M3 3l18 18M10.6 10.6A3 3 0 0012 15a3 3 0 002.4-4.4M9.9 5.2A10.5 10.5 0 0121 12a10.6 10.6 0 01-3.2 4.3M6.1 6.1A10.6 10.6 0 003 12a10.5 10.5 0 0012.8 6.7"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
              />
            </svg>
            <svg v-else viewBox="0 0 24 24" class="size-4" fill="none" aria-hidden="true">
              <path
                d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"
                stroke="currentColor"
                stroke-width="1.6"
              />
              <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.6" />
            </svg>
          </button>
        </div>
        <textarea
          class="mt-1.5 w-full cursor-pointer resize-y rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-xs text-ink outline-none"
          :key="`${entry.key}:${entry.value}:${isOpen(entry.key)}`"
          :rows="lineCount(displayValue(entry))"
          :value="displayValue(entry)"
          title="Click to copy"
          readonly
          spellcheck="false"
          @click="copyValue(entry, $event)"
        />
        <p v-if="copied === entry.key" class="mt-1 text-[10px] text-ok">Copied</p>
      </li>
    </ul>
  </section>
</template>
