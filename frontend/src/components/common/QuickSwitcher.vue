<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'

export interface SwitcherOption {
  id: string
  label: string
  hint?: string
}

const props = defineProps<{
  open: boolean
  title: string
  placeholder?: string
  options: SwitcherOption[]
}>()

const emit = defineEmits<{
  close: []
  select: [id: string]
}>()

const query = ref('')
const highlighted = ref(0)
const input = ref<HTMLInputElement>()

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return props.options
  return props.options.filter(
    (option) =>
      option.label.toLowerCase().includes(needle) ||
      option.hint?.toLowerCase().includes(needle),
  )
})

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    query.value = ''
    highlighted.value = 0
    await nextTick()
    input.value?.focus()
  },
)

watch(filtered, (list) => {
  if (highlighted.value >= list.length) highlighted.value = Math.max(0, list.length - 1)
})

function move(delta: number) {
  const count = filtered.value.length
  if (!count) return
  highlighted.value = (highlighted.value + delta + count) % count
}

function activate(index: number) {
  const option = filtered.value[index]
  if (!option) return
  emit('select', option.id)
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    move(1)
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    move(-1)
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    activate(highlighted.value)
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
  }
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-6 pt-24"
    @click.self="emit('close')"
  >
    <div class="w-full max-w-xl overflow-hidden rounded-2xl border border-line bg-surface-2 shadow-2xl">
      <p class="border-b border-line px-4 py-2 text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
        {{ title }}
      </p>
      <input
        ref="input"
        v-model="query"
        class="w-full border-b border-line bg-transparent px-4 py-3.5 text-sm text-ink outline-none placeholder:text-ink-faint"
        :placeholder="placeholder ?? 'Type to filter…'"
        spellcheck="false"
        @keydown="onKeydown"
      />

      <div class="max-h-96 overflow-y-auto p-1.5">
        <button
          v-for="(option, index) in filtered"
          :key="option.id"
          type="button"
          class="flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm"
          :class="index === highlighted ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-3'"
          @mouseenter="highlighted = index"
          @click="activate(index)"
        >
          <span class="truncate">{{ option.label }}</span>
          <span v-if="option.hint" class="shrink-0 text-[10px] uppercase tracking-widest text-ink-faint">
            {{ option.hint }}
          </span>
        </button>

        <p v-if="!filtered.length" class="px-3 py-6 text-center text-sm text-ink-faint">Nothing matches.</p>
      </div>

      <p class="border-t border-line px-4 py-2 text-[10px] text-ink-faint">
        ↑↓ move · ⏎ select · Esc close
      </p>
    </div>
  </div>
</template>
