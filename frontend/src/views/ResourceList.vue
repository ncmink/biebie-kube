<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import ResourceDrawer from '@/components/resource/ResourceDrawer.vue'
import ResourceTable from '@/components/resource/ResourceTable.vue'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import type { ResourceRow } from '@/types'

const props = defineProps<{ clusterId: string; kind: string }>()

const clusters = useClusterStore()
const resources = useResourceStore()

const namespace = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')
const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)
const selected = ref<ResourceRow | null>(null)

function reload() {
  void resources.load(props.clusterId, props.kind, namespace.value)
}

onMounted(reload)
watch([() => props.clusterId, () => props.kind, namespace], () => {
  selected.value = null
  reload()
})

function open(row: ResourceRow) {
  selected.value = row
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
      Secret values stay hidden. Opening a secret shows its keys; the eye
      reveals the stored base64 and does not decode it.
    </p>

    <div class="flex min-h-0 flex-1">
      <div class="min-w-0 flex-1">
        <p v-if="resources.error" class="m-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
          {{ resources.error }}
        </p>
        <p v-else-if="resources.loading" class="px-6 py-6 text-sm text-ink-muted">Loading…</p>
        <ResourceTable
          v-else-if="resources.page"
          :page="resources.page"
          :filter="resources.filter"
          :selected="selected"
          @open="open"
        />
      </div>
      <ResourceDrawer
        v-if="selected"
        :cluster-id="clusterId"
        :kind="kind"
        :row="selected"
        :kind-title="kindInfo?.title ?? kind"
        @close="selected = null"
      />
    </div>
  </div>
</template>
