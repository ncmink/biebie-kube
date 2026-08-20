<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

import type { ContextMenuItem } from '@/composables/menu'

const props = defineProps<{ x: number; y: number; items: ContextMenuItem[] }>()

const emit = defineEmits<{
  close: []
  select: [id: string]
}>()

/** Gap kept between the menu and the window edge. */
const margin = 8

const root = ref<HTMLElement | null>(null)
const left = ref(props.x)
const top = ref(props.y)
const placed = ref(false)

function close() {
  emit('close')
}

function choose(item: ContextMenuItem) {
  if (item.disabled) return
  emit('select', item.id)
}

function rows(): HTMLButtonElement[] {
  if (!root.value) return []
  return Array.from(root.value.querySelectorAll<HTMLButtonElement>('button:not([disabled])'))
}

/** Arrow keys move between rows, the way a native menu behaves. */
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  const buttons = rows()
  if (buttons.length === 0) return
  const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
  const step = event.key === 'ArrowDown' ? 1 : -1
  buttons[(current + step + buttons.length) % buttons.length].focus()
}

function onWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') close()
}

onMounted(() => {
  // The menu opens at the pointer, then folds back inside the window so a
  // right-click near an edge does not push rows out of reach.
  const el = root.value
  if (el) {
    const { width, height } = el.getBoundingClientRect()
    left.value = Math.max(margin, Math.min(props.x, window.innerWidth - width - margin))
    top.value = Math.max(margin, Math.min(props.y, window.innerHeight - height - margin))
  }
  placed.value = true
  rows()[0]?.focus()
  window.addEventListener('keydown', onWindowKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-50" @pointerdown="close" @wheel="close" @contextmenu.prevent="close">
      <div
        ref="root"
        role="menu"
        class="absolute min-w-56 rounded-xl border border-line bg-surface-2 p-1 shadow-2xl shadow-black/50"
        :style="{
          left: `${left}px`,
          top: `${top}px`,
          visibility: placed ? 'visible' : 'hidden',
        }"
        @pointerdown.stop
        @keydown="onKeydown"
      >
        <template v-for="(item, index) in items" :key="item.id">
          <div v-if="item.divider && index > 0" class="my-1 h-px bg-line" />
          <button
            type="button"
            role="menuitem"
            class="block w-full truncate rounded-lg px-3 py-1.5 text-left text-sm outline-none transition disabled:cursor-default disabled:opacity-40"
            :class="
              item.danger
                ? 'text-bad hover:bg-bad/10 focus-visible:bg-bad/10'
                : 'text-ink hover:bg-surface-3 focus-visible:bg-surface-3'
            "
            :disabled="item.disabled"
            @click="choose(item)"
          >
            {{ item.label }}
          </button>
        </template>
      </div>
    </div>
  </Teleport>
</template>
