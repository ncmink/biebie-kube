<script setup lang="ts">
import EnvironmentBadge from './EnvironmentBadge.vue'
import type { Cluster } from '@/types'

/**
 * Customer → Environment → Cluster, shown wherever an action could be aimed
 * at the wrong place. An engineer working four customers at once needs to read
 * where they are without thinking about it.
 */
defineProps<{ cluster: Cluster; compact?: boolean }>()
</script>

<template>
  <div class="flex min-w-0 items-center gap-2" :class="compact ? 'text-xs' : 'text-sm'">
    <span class="truncate font-semibold text-ink">{{ cluster.customerName || cluster.customerId || 'No customer' }}</span>
    <span class="text-ink-faint" aria-hidden="true">/</span>
    <span class="truncate text-ink-muted">{{ cluster.environmentName || 'No environment' }}</span>
    <span class="text-ink-faint" aria-hidden="true">/</span>
    <span class="truncate text-ink-muted">{{ cluster.name }}</span>
    <EnvironmentBadge :kind="cluster.environmentKind" />
  </div>
</template>
