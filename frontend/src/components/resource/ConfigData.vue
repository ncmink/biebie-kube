<script setup lang="ts">
import { ref, watch } from 'vue'

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

async function copyValue(entry: DataEntry, event: Event) {
  if (!isOpen(entry.key)) return
  const input = event.currentTarget as HTMLInputElement
  input.select()
  try {
    await navigator.clipboard.writeText(entry.value)
    copied.value = entry.key
    ui.say(`Copied ${entry.key}.`)
  } catch {
    ui.say('Could not copy to the clipboard.', 'bad')
  }
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
          <span v-if="entry.binary" class="text-[10px] uppercase tracking-wider text-ink-faint">
            binary
          </span>
          <button
            class="text-ink-faint hover:text-ink"
            :aria-label="isOpen(entry.key) ? `Hide ${entry.key}` : `Reveal ${entry.key}`"
            :title="
              sensitive
                ? 'Show the stored value. Secret data stays base64 — it is not decoded.'
                : 'Show or hide this value.'
            "
            @click="toggle(entry.key)"
          >
            <!-- Eye / eye-off. Reveal shows the stored encoding; never atob(). -->
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
        <input
          class="mt-1.5 w-full rounded-lg border border-line bg-surface-1 px-3 py-2 font-mono text-xs text-ink outline-none"
          :class="isOpen(entry.key) ? 'cursor-pointer' : 'cursor-default'"
          :type="isOpen(entry.key) ? 'text' : 'password'"
          :value="entry.value"
          :title="isOpen(entry.key) ? 'Click to copy' : undefined"
          readonly
          spellcheck="false"
          @click="copyValue(entry, $event)"
        />
        <p v-if="copied === entry.key" class="mt-1 text-[10px] text-ok">Copied</p>
      </li>
    </ul>
  </section>
</template>
