<script setup lang="ts">
import { computed } from 'vue'

import { useClusterStore } from '@/stores/clusters'

const props = defineProps<{ clusterId: string; disabled?: boolean }>()

const clusters = useClusterStore()

const namespaces = computed(() => clusters.namespaces[props.clusterId] ?? [])
const current = computed({
  get: () => clusters.sessions[props.clusterId]?.namespace ?? '',
  set: (value: string) => {
    void clusters.setNamespace(props.clusterId, value)
  },
})
</script>

<template>
  <div class="border-b border-line px-3 py-3">
    <label class="text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
      Namespace
    </label>
    <select
      v-model="current"
      class="mt-1.5 w-full rounded-lg border border-line bg-surface-2 px-2.5 py-1.5 text-sm text-ink outline-none focus:border-brand disabled:opacity-40"
      :disabled="disabled"
    >
      <!-- The empty value means every namespace, which is how the Go layer
           reads "no filter". -->
      <option value="">All namespaces</option>
      <option v-for="namespace in namespaces" :key="namespace" :value="namespace">
        {{ namespace }}
      </option>
    </select>
  </div>
</template>
