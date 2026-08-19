<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { useClusterStore } from '@/stores/clusters'
import type { KindInfo } from '@/types'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const route = useRoute()

/** The catalogue is filtered by the Go layer to what this cluster serves. */
const groups = computed(() => {
  const catalogue = clusters.catalogues[props.clusterId] ?? []
  const ordered = new Map<string, KindInfo[]>()
  for (const kind of catalogue) {
    ordered.set(kind.category, [...(ordered.get(kind.category) ?? []), kind])
  }
  return [...ordered.entries()]
})

const activeKind = computed(() => String(route.params.kind ?? ''))
</script>

<template>
  <nav class="overflow-y-auto px-2 py-3">
    <RouterLink
      :to="{ name: 'overview', params: { clusterId } }"
      class="block rounded-lg px-2.5 py-1.5 text-sm"
      :class="route.name === 'overview' ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-2'"
    >
      Overview
    </RouterLink>

    <div v-for="[category, kinds] in groups" :key="category" class="mt-4">
      <p class="px-2.5 text-[10px] font-semibold uppercase tracking-widest text-ink-faint">
        {{ category }}
      </p>
      <RouterLink
        v-for="kind in kinds"
        :key="kind.kind"
        :to="{ name: 'resources', params: { clusterId, kind: kind.kind } }"
        class="mt-0.5 block truncate rounded-lg px-2.5 py-1.5 text-sm"
        :class="activeKind === kind.kind ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-2'"
      >
        {{ kind.title }}
      </RouterLink>
    </div>

    <p v-if="!groups.length" class="px-2.5 py-4 text-xs text-ink-faint">
      Connect to load this cluster's resource types.
    </p>
  </nav>
</template>
