<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import StateDot from '@/components/common/StateDot.vue'
import { api, message } from '@/api'
import { age } from '@/composables/format'
import type { RelatedGroup, ResourceRef, ResourceRow } from '@/types'

const props = defineProps<{ clusterId: string; resource: ResourceRef }>()

const router = useRouter()

const groups = ref<RelatedGroup[]>([])
const loading = ref(false)
const error = ref('')

/**
 * token settles the race between two loads.
 *
 * Related objects cost a list where the inspector beside them costs a get, so
 * clicking down a list of deployments starts reads that finish out of order.
 * Only the newest one is allowed to write its answer.
 */
let token = 0

async function load() {
  const mine = ++token
  loading.value = true
  error.value = ''
  try {
    const found = await api.relatedResources(props.clusterId, props.resource)
    if (mine !== token) return
    groups.value = found
  } catch (err) {
    if (mine !== token) return
    error.value = message(err)
    groups.value = []
  } finally {
    if (mine === token) loading.value = false
  }
}

watch(
  () => [props.clusterId, props.resource.kind, props.resource.namespace, props.resource.name],
  load,
  { immediate: true },
)

function open(group: RelatedGroup, row: ResourceRow) {
  void router.push({
    name: 'resource',
    params: {
      clusterId: props.clusterId,
      kind: group.kind,
      // "_" stands in for "no namespace": a cluster-scoped object still needs
      // a path segment.
      namespace: row.namespace || '_',
      name: row.name,
    },
  })
}
</script>

<template>
  <div>
    <p v-if="error" class="text-xs text-bad">{{ error }}</p>
    <p v-else-if="loading" class="text-xs text-ink-muted">Loading…</p>

    <section v-for="group in groups" :key="group.title" class="mt-6 first:mt-0">
      <h2 class="mb-2 flex items-baseline gap-2">
        <span class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
          {{ group.title }}
        </span>
        <span class="font-mono text-[11px] text-ink-faint">
          {{ group.rows?.length ?? 0 }}<template v-if="group.truncated">+</template>
        </span>
      </h2>

      <div class="overflow-x-auto rounded-lg border border-line">
        <table class="w-full border-collapse whitespace-nowrap text-xs">
          <thead>
            <tr class="text-left text-[10px] uppercase tracking-wider text-ink-faint">
              <th class="px-3 py-1.5 font-medium">Name</th>
              <th v-if="group.namespaced" class="px-3 py-1.5 font-medium">Namespace</th>
              <th v-for="column in group.columns" :key="column.key" class="px-3 py-1.5 font-medium">
                {{ column.title }}
              </th>
              <th class="px-3 py-1.5 font-medium">Age</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in group.rows"
              :key="row.key"
              class="cursor-pointer border-t border-line/60 hover:bg-surface-2"
              @click="open(group, row)"
            >
              <td class="max-w-64 truncate px-3 py-1.5">
                <span class="flex items-center gap-2">
                  <StateDot :health="row.health" />
                  <span class="truncate text-ink">{{ row.name }}</span>
                </span>
              </td>
              <td v-if="group.namespaced" class="max-w-40 truncate px-3 py-1.5 text-ink-muted">
                {{ row.namespace }}
              </td>
              <td
                v-for="column in group.columns"
                :key="column.key"
                class="max-w-48 truncate px-3 py-1.5 text-ink-muted"
                :class="column.mono ? 'font-mono' : ''"
              >
                {{ row.fields?.[column.key] || '—' }}
              </td>
              <td class="px-3 py-1.5 font-mono text-ink-faint">{{ age(row.createdAt) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <p v-if="group.truncated" class="mt-1.5 text-[11px] text-ink-faint">
        Showing the first {{ group.rows?.length ?? 0 }}. Open the
        {{ group.title.toLowerCase() }} list for the rest.
      </p>
    </section>
  </div>
</template>
