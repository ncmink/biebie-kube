<script setup lang="ts">
/**
 * One square in the log toolbar.
 *
 * The toolbar carries a dozen controls above a pane that wants every pixel for
 * output, so they are icons rather than labels. The label survives as the
 * tooltip and as the accessible name, because an icon alone is a guessing game.
 */
withDefaults(defineProps<{ label: string; active?: boolean; disabled?: boolean }>(), {
  active: false,
  disabled: false,
})
</script>

<template>
  <button
    type="button"
    class="grid size-7 shrink-0 place-items-center rounded-lg transition-colors disabled:cursor-not-allowed disabled:opacity-40"
    :class="
      active
        ? 'bg-brand/15 text-brand hover:bg-brand/25'
        : 'text-ink-faint hover:bg-surface-3 hover:text-ink'
    "
    :disabled="disabled"
    :title="label"
    :aria-label="label"
    :aria-pressed="active"
  >
    <slot />
  </button>
</template>
