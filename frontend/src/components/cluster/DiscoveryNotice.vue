<script setup lang="ts">
import { computed, ref } from 'vue'

import { api, message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import type { DiscoverySnapshot } from '@/types'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const refreshing = ref(false)

const snapshot = computed<DiscoverySnapshot | undefined>(
  () => clusters.discovery[props.clusterId],
)

const incomplete = computed(() => snapshot.value && !snapshot.value.complete)
const issues = computed(() => snapshot.value?.issues ?? [])

async function refresh() {
  refreshing.value = true
  try {
    await clusters.refreshCatalogue(props.clusterId)
  } catch (error) {
    message(error)
  } finally {
    refreshing.value = false
  }
}
</script>

<template>
  <div
    v-if="incomplete || issues.length"
    class="border-b border-line bg-surface-1 px-3 py-2 text-xs text-ink-muted"
  >
    <div class="flex items-start justify-between gap-3">
      <div>
        <p class="font-medium text-ink">Some API groups are unavailable</p>
        <ul v-if="issues.length" class="mt-1 list-disc pl-4">
          <li v-for="(issue, index) in issues" :key="`${issue.code}-${issue.group}-${index}`">
            <span v-if="issue.group" class="font-mono">{{ issue.group }}</span>
            <span v-if="issue.group"> — </span>
            {{ issue.message }}
          </li>
        </ul>
        <p v-else class="mt-1">Discovery did not finish completely. Built-in kinds may be unverified.</p>
      </div>
      <button
        class="shrink-0 rounded-md border border-line px-2 py-1 text-[11px] font-semibold hover:bg-surface-2"
        :disabled="refreshing"
        @click="refresh"
      >
        {{ refreshing ? 'Refreshing…' : 'Refresh' }}
      </button>
    </div>
  </div>
</template>
