<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import { AccessMode } from '@/types'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const ui = useUIStore()

const policy = computed(() => clusters.policies[props.clusterId])
const readOnly = computed(
  () => policy.value?.effectiveMode === AccessMode.AccessModeReadOnly,
)
const toggling = ref(false)

const badge = computed(() => {
  if (!readOnly.value || !policy.value) return ''
  if (policy.value.persistedMode === AccessMode.AccessModeReadOnly && policy.value.sessionReadOnly) {
    return 'cluster default and this session'
  }
  if (policy.value.persistedMode === AccessMode.AccessModeReadOnly) {
    return 'cluster default'
  }
  return 'this session'
})

const canToggleSession = computed(
  () => policy.value?.persistedMode !== AccessMode.AccessModeReadOnly && !toggling.value,
)

watch(
  () => props.clusterId,
  (id) => {
    if (id) void clusters.loadPolicy(id)
  },
  { immediate: true },
)

async function toggleSession() {
  if (!policy.value || !canToggleSession.value) return
  const next = !policy.value.sessionReadOnly
  toggling.value = true
  try {
    await clusters.setSessionReadOnly(props.clusterId, next)
    ui.say(
      next
        ? 'This session is read-only until you disconnect.'
        : 'This session is read-write again.',
    )
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    toggling.value = false
  }
}
</script>

<template>
  <div
    v-if="readOnly"
    class="flex shrink-0 items-center justify-between gap-3 border-b border-warn/30 bg-warn/10 px-4 py-2"
  >
    <p class="text-xs text-warn">
      <span class="font-semibold uppercase tracking-wide">Read-only</span>
      ({{ badge }}). Mutations, exec, port forwards and Argo actions are blocked.
    </p>
    <button
      v-if="canToggleSession && policy?.sessionReadOnly"
      class="shrink-0 rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
      @click="toggleSession"
    >
      Allow writes this session
    </button>
  </div>

  <div
    v-else-if="canToggleSession"
    class="flex shrink-0 items-center justify-end border-b border-line bg-surface-1 px-4 py-1.5"
  >
    <button
      class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
      @click="toggleSession"
    >
      Make this session read-only
    </button>
  </div>
</template>
