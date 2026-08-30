<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { age } from '@/composables/format'
import type { Column, ResourceRow } from '@/types'

const props = defineProps<{
  /**
   * identity changes when the table becomes a different one — another cluster,
   * kind or namespace. Rows changing is not that: a rollout replaces rows many
   * times a second, and treating each as a new table is what threw the user
   * back to the top of the list while they were reading it.
   */
  identity: string
  rows: ResourceRow[]
  columns: Column[]
  namespaced: boolean
  sortKey: string
  sortDesc: boolean
  selected?: ResourceRow | null
}>()

const emit = defineEmits<{
  open: [row: ResourceRow]
  menu: [row: ResourceRow, event: MouseEvent]
  sort: [key: string]
  end: []
}>()

/**
 * Windowing keeps the DOM small.
 *
 * A namespace with several thousand pods would otherwise create a row element
 * for each one, and the table would take seconds to appear and stutter while
 * scrolling. Only what fits on screen, plus a margin, is rendered.
 */
const rowHeight = 34
const overscan = 12

/** How close to the end of the loaded rows asks for the next window. */
const prefetchRows = 60

const viewport = ref<HTMLElement>()
const scrollTop = ref(0)
const viewportHeight = ref(600)

const start = computed(() => Math.max(0, Math.floor(scrollTop.value / rowHeight) - overscan))
const visibleCount = computed(() => Math.ceil(viewportHeight.value / rowHeight) + overscan * 2)
const visible = computed(() => props.rows.slice(start.value, start.value + visibleCount.value))

const sortable = computed(() => [
  { key: 'name', title: 'Name' },
  ...(props.namespaced ? [{ key: 'namespace', title: 'Namespace' }] : []),
  ...props.columns.map((column) => ({ key: column.key, title: column.title })),
  { key: 'createdAt', title: 'Age' },
])

function onScroll(event: Event) {
  const element = event.target as HTMLElement
  scrollTop.value = element.scrollTop

  if (start.value + visibleCount.value + prefetchRows >= props.rows.length) emit('end')
}

function measure() {
  if (viewport.value) viewportHeight.value = viewport.value.clientHeight
}

// Only a different table starts at the top. Everything else — a patched row, a
// window that grew, a re-sort — leaves the scroll where the user put it.
watch(
  () => props.identity,
  () => {
    scrollTop.value = 0
    if (viewport.value) viewport.value.scrollTop = 0
    measure()
  },
)

// The window is sized from the viewport, so a resized window — or a drawer
// opening beside the table — has to re-measure or the rows run out halfway down
// a taller table.
const resizes = new ResizeObserver(measure)

watch(viewport, (element) => {
  resizes.disconnect()
  if (element) resizes.observe(element)
  measure()
})
onUnmounted(() => resizes.disconnect())

function isSelected(row: ResourceRow): boolean {
  return props.selected?.namespace === row.namespace && props.selected?.name === row.name
}
</script>

<template>
  <div ref="viewport" class="h-full overflow-auto" @scroll.passive="onScroll">
    <table class="w-full border-collapse text-sm">
      <thead class="sticky top-0 z-10 bg-surface-1">
        <tr class="text-left text-[11px] uppercase tracking-wider text-ink-faint">
          <th
            v-for="(column, index) in sortable"
            :key="column.key"
            class="font-medium"
            :class="index === 0 ? 'px-6 py-2' : 'px-3 py-2'"
          >
            <button
              class="flex items-center gap-1 hover:text-ink"
              :class="sortKey === column.key ? 'text-ink' : ''"
              @click="emit('sort', column.key)"
            >
              <span class="truncate">{{ column.title }}</span>
              <svg
                v-if="sortKey === column.key"
                viewBox="0 0 16 16"
                class="size-3 shrink-0"
                :class="sortDesc ? '' : 'rotate-180'"
                fill="none"
                aria-hidden="true"
              >
                <path d="M8 4v8M4.5 8.5L8 12l3.5-3.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
              </svg>
            </button>
          </th>
        </tr>
      </thead>

      <tbody>
        <tr v-if="start > 0" :style="{ height: `${start * rowHeight}px` }" aria-hidden="true">
          <td :colspan="sortable.length" />
        </tr>

        <tr
          v-for="row in visible"
          :key="row.key"
          class="cursor-pointer border-t border-line/60 hover:bg-surface-2"
          :class="isSelected(row) ? 'bg-brand/10' : ''"
          :style="{ height: `${rowHeight}px` }"
          @click="emit('open', row)"
          @contextmenu.prevent="emit('menu', row, $event)"
        >
          <td class="max-w-80 truncate px-6">
            <span class="flex items-center gap-2">
              <StateDot :health="row.health" />
              <span class="truncate text-ink">{{ row.name }}</span>
            </span>
          </td>
          <td v-if="namespaced" class="max-w-40 truncate px-3 text-ink-muted">
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
          <td :colspan="sortable.length" />
        </tr>
      </tbody>
    </table>

    <p v-if="!rows.length" class="px-6 py-10 text-center text-sm text-ink-faint">
      Nothing to show here.
    </p>
  </div>
</template>
