<script setup lang="ts">
import { shortcutCatalog } from '@/composables/shortcutCatalog'
import { useUIStore } from '@/stores/ui'

const ui = useUIStore()
</script>

<template>
  <div
    v-if="ui.shortcutsOpen"
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-6 pt-16"
    @click.self="ui.shortcutsOpen = false"
  >
    <div
      class="max-h-[85vh] w-full max-w-2xl overflow-hidden rounded-2xl border border-line bg-surface-2 shadow-2xl"
      role="dialog"
      aria-label="Keyboard shortcuts"
    >
      <div class="flex items-center justify-between border-b border-line px-5 py-3">
        <h2 class="text-sm font-semibold text-ink">Keyboard shortcuts</h2>
        <button
          type="button"
          class="rounded-lg px-2 py-1 text-xs text-ink-muted hover:text-ink"
          @click="ui.shortcutsOpen = false"
        >
          Esc
        </button>
      </div>

      <div class="max-h-[calc(85vh-3rem)] overflow-y-auto px-5 py-4">
        <section v-for="group in shortcutCatalog" :key="group.title" class="mb-6 last:mb-0">
          <h3 class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
            {{ group.title }}
          </h3>
          <dl class="space-y-1.5">
            <div
              v-for="item in group.items"
              :key="item.label"
              class="flex items-baseline justify-between gap-4 text-sm"
            >
              <dt class="text-ink-muted">{{ item.label }}</dt>
              <dd class="shrink-0 font-mono text-xs text-ink-faint">{{ item.keys }}</dd>
            </div>
          </dl>
        </section>
      </div>
    </div>
  </div>
</template>
