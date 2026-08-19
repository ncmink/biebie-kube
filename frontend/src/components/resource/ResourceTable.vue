<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { age } from '@/composables/format'
import type { ResourcePage, ResourceRow } from '@/types'

const props = defineProps<{ page: ResourcePage; filter?: string; selected?: ResourceRow | null }>()
const emit = defineEmits<{ open: [row: ResourceRow] }>()

// A page with nothing on it arrives with null rows, since that is what Go's nil
// slice marshals to. Everything below treats it as an empty table.
const allRows = computed(() => props.page.rows ?? [])
const columns = computed(() => props.page.columns ?? [])

const rows = computed(() => {
  const needle = (props.filter ?? '').trim().toLowerCase()
  if (!needle) return allRows.value
  return allRows.value.filter((row) => row.name.toLowerCase().includes(needle))
})

/**
 * Windowing keeps the DOM small.
 *
 * A namespace with several thousand pods would otherwise create a row element
 * for each one, and the table would take seconds to appear and stutter while
 * scrolling. Only what fits on screen, plus a margin, is rendered.
 */
const rowHeight = 34
const overscan = 12

const viewport = ref<HTMLElement>()
const scrollTop = ref(0)
const viewportHeight = ref(600)

const start = computed(() => Math.max(0, Math.floor(scrollTop.value / rowHeight) - overscan))
const visibleCount = computed(() => Math.ceil(viewportHeight.value / rowHeight) + overscan * 2)
const visible = computed(() => rows.value.slice(start.value, start.value + visibleCount.value))

function onScroll(event: Event) {
  scrollTop.value = (event.target as HTMLElement).scrollTop
}

function measure() {
  if (viewport.value) viewportHeight.value = viewport.value.clientHeight
}

watch(
  () => props.page,
  () => {
    scrollTop.value = 0
    if (viewport.value) viewport.value.scrollTop = 0
    measure()
  },
)

watch(viewport, measure)
</script>

<template>
  <div ref="viewport" class="h-full overflow-auto" @scroll="onScroll">
    <table class="w-full border-collapse text-sm">
      <thead class="sticky top-0 z-10 bg-surface-1">
        <tr class="text-left text-[11px] uppercase tracking-wider text-ink-faint">
          <th class="px-6 py-2 font-medium">Name</th>
          <th v-if="page.namespaced" class="px-3 py-2 font-medium">Namespace</th>
          <th v-for="column in columns" :key="column.key" class="px-3 py-2 font-medium">
            {{ column.title }}
          </th>
          <th class="px-3 py-2 font-medium">Age</th>
        </tr>
      </thead>

      <tbody>
        <tr v-if="start > 0" :style="{ height: `${start * rowHeight}px` }" aria-hidden="true">
          <td :colspan="columns.length + 3" />
        </tr>

        <tr
          v-for="row in visible"
          :key="row.uid || `${row.namespace}/${row.name}`"
          class="cursor-pointer border-t border-line/60 hover:bg-surface-2"
          :class="
            selected && selected.namespace === row.namespace && selected.name === row.name
              ? 'bg-brand/10'
              : ''
          "
          :style="{ height: `${rowHeight}px` }"
          @click="emit('open', row)"
        >
          <td class="max-w-80 truncate px-6">
            <span class="flex items-center gap-2">
              <StateDot :health="row.health" />
              <span class="truncate text-ink">{{ row.name }}</span>
            </span>
          </td>
          <td v-if="page.namespaced" class="max-w-40 truncate px-3 text-ink-muted">
            {{ row.namespace }}
          </td>
          <td
            v-for="column in columns"
            :key="column.key"
            class="max-w-64 truncate px-3 text-ink-muted"
            :class="column.mono ? 'font-mono text-xs' : ''"
          >
            {{ row.fields?.[column.key] ?? '—' }}
          </td>
          <td class="px-3 font-mono text-xs text-ink-faint">{{ age(row.createdAt) }}</td>
        </tr>

        <tr
          v-if="rows.length > start + visibleCount"
          :style="{ height: `${(rows.length - start - visibleCount) * rowHeight}px` }"
          aria-hidden="true"
        >
          <td :colspan="columns.length + 3" />
        </tr>
      </tbody>
    </table>

    <p v-if="!rows.length" class="px-6 py-10 text-center text-sm text-ink-faint">
      Nothing to show here.
    </p>

    <p v-if="page.truncated" class="border-t border-line px-6 py-2 text-xs text-warn">
      This list is very large and has been cut short. Narrow it with a namespace or a filter.
    </p>
  </div>
</template>
