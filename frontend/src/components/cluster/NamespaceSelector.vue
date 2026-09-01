<script setup lang="ts">
/**
 * Choosing the namespace everything else in the window is scoped to.
 *
 * Written rather than a native `<select>` because a `<select>` has no filter,
 * and a customer cluster with two hundred namespaces is a list nobody can find
 * `reporting` in by scrolling. And the platform draws its own popup: on macOS
 * that is a system panel in the system's colours, sized to the longest entry.
 *
 * The list floats over the resource nav rather than pushing it down, because
 * a sidebar that reflows every time someone opens a picker is disorienting.
 * It scrolls past a fixed height rather than growing, so a cluster with two
 * hundred namespaces covers the same corner of the nav as one with six.
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

import { useClusterStore } from '@/stores/clusters'

const props = defineProps<{ clusterId: string; disabled?: boolean }>()

const clusters = useClusterStore()

/** The empty value means every namespace, which is how the Go layer reads
 *  "no filter". */
const everything = { value: '', label: 'All namespaces' }

const root = ref<HTMLElement>()
const search = ref<HTMLInputElement>()
const list = ref<HTMLElement>()

const open = ref(false)
const query = ref('')
/** -1 means nothing is highlighted, so Enter on a freshly opened list does not
 *  quietly change the namespace the whole window is scoped to. */
const highlighted = ref(-1)

const namespaces = computed(() => clusters.namespaces[props.clusterId] ?? [])
const current = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')

const options = computed(() => [
  everything,
  ...namespaces.value.map((namespace) => ({ value: namespace, label: namespace })),
])

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return options.value
  return options.value.filter((option) => option.label.toLowerCase().includes(needle))
})

function choose(value: string) {
  open.value = false
  if (value === current.value) return
  void clusters.setNamespace(props.clusterId, value)
}

function commit() {
  const option = filtered.value[highlighted.value]
  if (option) choose(option.value)
}

async function toggle() {
  if (props.disabled) return
  open.value = !open.value
  if (!open.value) return

  highlighted.value = filtered.value.findIndex((option) => option.value === current.value)
  await nextTick()
  search.value?.focus()
  // The namespace in use is often far down a long list, so the picker opens
  // scrolled to it rather than at the top.
  reveal()
}

function move(delta: number) {
  const count = filtered.value.length
  if (!count) return
  if (highlighted.value < 0) highlighted.value = delta > 0 ? 0 : count - 1
  else highlighted.value = (highlighted.value + delta + count) % count
  reveal()
}

/** Keeps the highlighted row inside the scrolling area, so arrow keys walk off
 *  the bottom edge instead of leaving the highlight somewhere unseen. */
function reveal() {
  const row = list.value?.children[highlighted.value]
  row?.scrollIntoView({ block: 'nearest' })
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') open.value = false
}

function onWindowPointerdown(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) open.value = false
}

// Typing narrows the list, so whatever was highlighted may no longer be in it.
// The first match leads, because that is what the person is typing towards.
watch(query, () => {
  highlighted.value = filtered.value.length ? 0 : -1
  void nextTick(reveal)
})

watch(open, (isOpen) => {
  if (isOpen) {
    window.addEventListener('keydown', onWindowKeydown)
    window.addEventListener('pointerdown', onWindowPointerdown)
    return
  }
  window.removeEventListener('keydown', onWindowKeydown)
  window.removeEventListener('pointerdown', onWindowPointerdown)
  // Cleared on the way out rather than on the way in, so opening can highlight
  // the namespace in use without the filter watcher overwriting it.
  query.value = ''
})

// A cluster that disconnects while the list is open has no namespaces left to
// choose from, and a list of the last session's is worse than none.
watch(
  () => props.disabled,
  (isDisabled) => {
    if (isDisabled) open.value = false
  },
)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
  window.removeEventListener('pointerdown', onWindowPointerdown)
})
</script>

<template>
  <div ref="root" class="shrink-0 border-b border-line px-3 py-3">
    <label class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
      Namespace
    </label>

    <div class="relative mt-1.5">
      <button
        type="button"
        class="flex w-full items-center gap-2 rounded-lg border bg-surface-2 px-2.5 py-1.5 text-left text-sm outline-none hover:border-line-strong focus-visible:border-brand disabled:opacity-40"
        :class="open ? 'border-brand' : 'border-line'"
        :disabled="disabled"
        @click="toggle"
      >
        <span class="truncate" :class="current ? 'text-ink' : 'text-ink-muted'">
          {{ current || everything.label }}
        </span>
        <svg
          viewBox="0 0 16 16"
          class="ml-auto size-4 shrink-0 text-ink-faint transition-transform"
          :class="open ? 'rotate-180' : ''"
          fill="none"
          aria-hidden="true"
        >
          <path d="M4 6.5l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>

      <!-- Hangs off the trigger so the nav underneath keeps its place. Allowed
           to run wider than the sidebar, because a namespace picker that
           truncates namespace names is not much of a picker. -->
      <div
        v-if="open"
        class="absolute left-0 top-full z-30 mt-1.5 w-max min-w-full max-w-[340px] overflow-hidden rounded-lg border border-line bg-surface-2 shadow-xl shadow-black/40"
      >
        <input
          ref="search"
          v-model="query"
          class="w-full border-b border-line bg-transparent px-2.5 py-1.5 text-sm text-ink outline-none placeholder:text-ink-faint"
          placeholder="Filter…"
          spellcheck="false"
          autocomplete="off"
          @keydown.down.prevent="move(1)"
          @keydown.up.prevent="move(-1)"
          @keydown.enter.prevent="commit"
          @keydown.esc.prevent="open = false"
        />

        <div ref="list" class="max-h-56 overflow-y-auto p-1">
          <button
            v-for="(option, index) in filtered"
            :key="option.value"
            type="button"
            class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm"
            :class="index === highlighted ? 'bg-brand/15 text-ink' : 'text-ink-muted'"
            @mouseenter="highlighted = index"
            @click="choose(option.value)"
          >
            <span class="truncate" :class="option.value ? '' : 'italic'">{{ option.label }}</span>
            <span v-if="option.value === current" class="ml-auto shrink-0 text-[10px] text-brand">
              current
            </span>
          </button>
        </div>

        <p v-if="!filtered.length" class="px-2.5 py-2 text-center text-xs text-ink-faint">
          No namespace matches “{{ query }}”.
        </p>

        <p class="border-t border-line px-2.5 py-1 text-[10px] text-ink-faint">
          {{ namespaces.length }} namespace{{ namespaces.length === 1 ? '' : 's' }}
        </p>
      </div>
    </div>
  </div>
</template>
