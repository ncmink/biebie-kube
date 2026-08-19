<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { api, message } from '@/api'
import { age } from '@/composables/format'
import { Health } from '@/types'
import type { EventRow } from '@/types'

const props = defineProps<{ clusterId: string; namespace: string; involving?: string }>()

const rows = ref<EventRow[]>([])
const error = ref('')
const onlyWarnings = ref(false)
const filter = ref('')

const shown = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  return rows.value.filter((row) => {
    if (onlyWarnings.value && row.type !== 'Warning') return false
    if (!needle) return true
    return (
      row.message.toLowerCase().includes(needle) ||
      row.reason.toLowerCase().includes(needle) ||
      row.object.toLowerCase().includes(needle)
    )
  })
})

async function load() {
  error.value = ''
  try {
    rows.value = await api.events(props.clusterId, props.namespace, props.involving ?? '')
  } catch (err) {
    error.value = message(err)
  }
}

onMounted(load)
watch(() => [props.clusterId, props.namespace, props.involving], load)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-3 border-b border-line px-6 py-2">
      <label class="flex items-center gap-1.5 text-xs text-ink-muted">
        <input v-model="onlyWarnings" type="checkbox" class="accent-brand" /> Warnings only
      </label>
      <input
        v-model="filter"
        class="w-56 rounded-lg border border-line bg-surface-2 px-2 py-1 text-xs text-ink outline-none focus:border-brand"
        placeholder="Filter events"
        spellcheck="false"
      />
      <button
        class="ml-auto rounded-lg border border-line px-2 py-1 text-xs text-ink-muted hover:text-ink"
        @click="load"
      >
        Refresh
      </button>
    </div>

    <p v-if="error" class="m-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
      {{ error }}
    </p>

    <div v-else class="min-h-0 flex-1 overflow-y-auto">
      <p v-if="!shown.length" class="px-6 py-10 text-center text-sm text-ink-faint">
        No events to show.
      </p>
      <ul v-else class="divide-y divide-line">
        <li v-for="row in shown" :key="row.uid" class="flex items-start gap-3 px-6 py-2.5">
          <StateDot
            :health="row.type === 'Warning' ? Health.HealthWarning : Health.HealthHealthy"
            class="mt-1.5"
          />
          <div class="min-w-0 flex-1">
            <p class="text-sm text-ink">
              <span class="font-medium">{{ row.reason }}</span>
              <span class="text-ink-faint"> · {{ row.object }}</span>
            </p>
            <p class="mt-0.5 text-xs leading-relaxed text-ink-muted">{{ row.message }}</p>
          </div>
          <div class="shrink-0 text-right">
            <p class="font-mono text-xs text-ink-faint">{{ age(row.lastSeen) }}</p>
            <p v-if="row.count > 1" class="font-mono text-[10px] text-ink-faint">×{{ row.count }}</p>
          </div>
        </li>
      </ul>
    </div>
  </div>
</template>
