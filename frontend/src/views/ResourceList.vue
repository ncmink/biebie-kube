<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'

import ResourceTable from '@/components/resource/ResourceTable.vue'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import type { ResourceRow } from '@/types'

const props = defineProps<{ clusterId: string; kind: string }>()

const clusters = useClusterStore()
const resources = useResourceStore()
const router = useRouter()

const namespace = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')
const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)

function reload() {
  void resources.load(props.clusterId, props.kind, namespace.value)
}

onMounted(reload)
watch([() => props.clusterId, () => props.kind, namespace], reload)

function open(row: ResourceRow) {
  void router.push({
    name: 'resource',
    params: {
      clusterId: props.clusterId,
      kind: props.kind,
      // The router needs a path segment even for a cluster-scoped object.
      namespace: row.namespace || '_',
      name: row.name,
    },
  })
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header class="flex shrink-0 items-center gap-3 border-b border-line px-6 py-3">
      <h1 class="text-sm font-semibold text-ink">{{ kindInfo?.title ?? kind }}</h1>
      <span v-if="resources.page" class="font-mono text-xs text-ink-faint">
        {{ resources.page.rows?.length ?? 0 }}
      </span>
      <input
        v-model="resources.filter"
        class="ml-auto w-64 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
        placeholder="Filter by name"
        spellcheck="false"
      />
      <button
        class="rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="reload"
      >
        Refresh
      </button>
    </header>

    <p
      v-if="kindInfo?.sensitive"
      class="shrink-0 border-b border-line bg-warn/10 px-6 py-2 text-xs text-warn"
    >
      Secret values stay hidden. Opening a secret shows its keys; revealing a value takes a
      deliberate action.
    </p>

    <div class="min-h-0 flex-1">
      <p v-if="resources.error" class="m-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
        {{ resources.error }}
      </p>
      <p v-else-if="resources.loading" class="px-6 py-6 text-sm text-ink-muted">Loading…</p>
      <ResourceTable
        v-else-if="resources.page"
        :page="resources.page"
        :filter="resources.filter"
        @open="open"
      />
    </div>
  </div>
</template>
