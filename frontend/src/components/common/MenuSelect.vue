<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

export type MenuSelectOption = {
  value: string
  label: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    options: MenuSelectOption[]
    label: string
    placeholder?: string
    searchable?: boolean
    searchPlaceholder?: string
    emptyLabel?: string
    disabled?: boolean
  }>(),
  {
    placeholder: 'Choose…',
    searchPlaceholder: 'Filter…',
    emptyLabel: 'No options found.',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const root = ref<HTMLElement>()
const search = ref<HTMLInputElement>()
const list = ref<HTMLElement>()
const open = ref(false)
const query = ref('')
const highlighted = ref(-1)

const selected = computed(() => props.options.find((option) => option.value === props.modelValue))
const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!props.searchable || !needle) return props.options
  return props.options.filter((option) => option.label.toLowerCase().includes(needle))
})

function reveal() {
  const row = list.value?.children[highlighted.value]
  row?.scrollIntoView({ block: 'nearest' })
}

function move(delta: number) {
  const count = filtered.value.length
  if (!count) return
  if (highlighted.value < 0) highlighted.value = delta > 0 ? 0 : count - 1
  else highlighted.value = (highlighted.value + delta + count) % count
  reveal()
}

function choose(value: string) {
  if (value !== props.modelValue) emit('update:modelValue', value)
  open.value = false
}

function commit() {
  const option = filtered.value[highlighted.value]
  if (option) choose(option.value)
}

async function show() {
  if (props.disabled || open.value) return
  open.value = true
  highlighted.value = filtered.value.findIndex((option) => option.value === props.modelValue)
  if (highlighted.value < 0 && !props.searchable && filtered.value.length) highlighted.value = 0
  await nextTick()
  if (props.searchable) search.value?.focus()
  reveal()
}

function toggle() {
  if (open.value) {
    open.value = false
    return
  }
  void show()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!open.value) {
      void show()
      return
    }
    move(event.key === 'ArrowDown' ? 1 : -1)
    return
  }
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    if (open.value) commit()
    else void show()
    return
  }
  if (event.key === 'Escape') open.value = false
}

function onWindowPointerdown(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) open.value = false
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') open.value = false
}

watch(query, () => {
  highlighted.value = filtered.value.length ? 0 : -1
  void nextTick(reveal)
})

watch(open, (isOpen) => {
  if (isOpen) {
    window.addEventListener('pointerdown', onWindowPointerdown)
    window.addEventListener('keydown', onWindowKeydown)
    return
  }
  window.removeEventListener('pointerdown', onWindowPointerdown)
  window.removeEventListener('keydown', onWindowKeydown)
  query.value = ''
})

watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) open.value = false
  },
)

onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', onWindowPointerdown)
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <div ref="root" class="relative min-w-0">
    <button
      type="button"
      class="flex w-full items-center gap-2 rounded-lg border bg-surface-3 px-2.5 py-2 text-left text-xs outline-none hover:border-line-strong focus-visible:border-brand disabled:opacity-40"
      :class="open ? 'border-brand' : 'border-line'"
      :disabled="disabled"
      :aria-label="label"
      :aria-expanded="open"
      aria-haspopup="listbox"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span class="truncate" :class="selected ? 'text-ink' : 'text-ink-faint'">
        {{ selected?.label ?? placeholder }}
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

    <div
      v-if="open"
      class="absolute left-0 top-full z-30 mt-1.5 w-max min-w-full max-w-[340px] overflow-hidden rounded-lg border border-line bg-surface-2 shadow-xl shadow-black/40"
    >
      <input
        v-if="searchable"
        ref="search"
        v-model="query"
        class="w-full border-b border-line bg-transparent px-2.5 py-1.5 text-sm text-ink outline-none placeholder:text-ink-faint"
        :placeholder="searchPlaceholder"
        :aria-label="`Search ${label.toLowerCase()}`"
        spellcheck="false"
        autocomplete="off"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="commit"
        @keydown.esc.prevent="open = false"
      />

      <div ref="list" class="max-h-56 overflow-y-auto p-1" role="listbox" :aria-label="label">
        <button
          v-for="(option, index) in filtered"
          :key="option.value"
          type="button"
          role="option"
          class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm"
          :class="index === highlighted ? 'bg-brand/15 text-ink' : 'text-ink-muted'"
          :aria-selected="option.value === modelValue"
          @mouseenter="highlighted = index"
          @click="choose(option.value)"
        >
          <span class="truncate">{{ option.label }}</span>
          <span
            v-if="option.value === modelValue"
            class="ml-auto shrink-0 text-[10px] text-brand"
          >
            current
          </span>
        </button>
      </div>

      <p v-if="!filtered.length" class="px-2.5 py-2 text-center text-xs text-ink-faint">
        {{ emptyLabel }}
      </p>
    </div>
  </div>
</template>
