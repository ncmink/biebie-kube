<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

import { api } from '@/api'
import { useUIStore } from '@/stores/ui'

const ui = useUIStore()

const dismissMarksSeen = ref(false)
let returnFocus: HTMLElement | null = null

const notes = computed(() => ui.whatsNewNotes)

const headline = computed(() => {
  const list = notes.value
  if (list.length === 1) return `What's new in BiebieKube ${list[0].version}`
  if (list.length > 1) {
    const oldest = list[list.length - 1]?.version
    const newest = list[0]?.version
    return `What's new in BiebieKube ${oldest}–${newest}`
  }
  return "What's new"
})

function close() {
  ui.closeWhatsNew()
}

async function gotIt() {
  if (dismissMarksSeen.value) {
    try {
      await api.markReleaseNotesSeen()
    } catch {
      // Dismissing should still close the sheet.
    }
  }
  close()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    void gotIt()
  }
}

watch(
  () => ui.whatsNewOpen,
  (open) => {
    if (open) {
      returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
      dismissMarksSeen.value = ui.whatsNewMarkOnDismiss
      return
    }
    dismissMarksSeen.value = false
    returnFocus?.focus()
    returnFocus = null
  },
)

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div
    v-if="ui.whatsNewOpen"
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-6 pt-16"
    @click.self="gotIt"
  >
    <div
      class="max-h-[85vh] w-full max-w-xl overflow-hidden rounded-2xl border border-line bg-surface-2 shadow-2xl"
      role="dialog"
      aria-labelledby="whats-new-title"
    >
      <div class="flex items-center justify-between border-b border-line px-5 py-3">
        <h2 id="whats-new-title" class="text-sm font-semibold text-ink">{{ headline }}</h2>
        <button
          type="button"
          class="rounded-lg px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="gotIt"
        >
          Esc
        </button>
      </div>

      <div class="max-h-[calc(85vh-7rem)] overflow-y-auto px-5 py-4">
        <section
          v-for="note in notes"
          :key="note.version"
          class="mb-6 border-b border-line pb-6 last:mb-0 last:border-b-0 last:pb-0"
        >
          <div class="mb-3 flex items-baseline justify-between gap-3">
            <h3 class="font-mono text-sm font-semibold text-ink">v{{ note.version }}</h3>
            <time class="text-[11px] text-ink-faint">{{ note.date }}</time>
          </div>
          <ul class="space-y-3">
            <li v-for="(item, index) in note.highlights" :key="index">
              <p class="text-sm font-medium text-ink">{{ item.title }}</p>
              <p v-if="item.detail" class="mt-0.5 text-sm text-ink-muted">{{ item.detail }}</p>
            </li>
          </ul>
          <ul v-if="note.fixes?.length" class="mt-3 list-disc space-y-1 pl-5 text-sm text-ink-muted">
            <li v-for="(fix, index) in note.fixes" :key="index">{{ fix }}</li>
          </ul>
        </section>
      </div>

      <div class="flex justify-end border-t border-line px-5 py-3">
        <button
          type="button"
          class="rounded-lg bg-brand px-4 py-2 text-sm font-medium text-white hover:opacity-90"
          @click="gotIt"
        >
          Got it
        </button>
      </div>
    </div>
  </div>
</template>
