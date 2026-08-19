<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import StateDot from './StateDot.vue'
import { api, message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import type { SearchHit } from '@/types'

interface Command {
  id: string
  title: string
  hint?: string
  run: () => void | Promise<void>
}

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()
const route = useRoute()

const query = ref('')
const highlighted = ref(0)
const hits = ref<SearchHit[]>([])
const input = ref<HTMLInputElement>()

const clusterId = computed(() => String(route.params.clusterId ?? clusters.activeId ?? ''))
const connected = computed(() => clusters.sessions[clusterId.value]?.state === 'connected')

/**
 * Commands are built from what is actually available right now, so the palette
 * never offers an action that would fail — switching namespace on a cluster
 * that is not connected, for instance.
 */
const commands = computed<Command[]>(() => {
  const list: Command[] = []

  for (const cluster of clusters.clusters) {
    list.push({
      id: `cluster:${cluster.id}`,
      title: `Open ${cluster.customerName || cluster.customerId} / ${cluster.name}`,
      hint: 'Cluster',
      run: async () => {
        await clusters.open(cluster.id)
        await router.push({ name: 'overview', params: { clusterId: cluster.id } })
      },
    })
  }

  if (connected.value) {
    for (const namespace of clusters.namespaces[clusterId.value] ?? []) {
      list.push({
        id: `ns:${namespace}`,
        title: `Switch to namespace ${namespace}`,
        hint: 'Namespace',
        run: () => clusters.setNamespace(clusterId.value, namespace),
      })
    }
    for (const kind of clusters.catalogues[clusterId.value] ?? []) {
      list.push({
        id: `kind:${kind.kind}`,
        title: `Open ${kind.title}`,
        hint: kind.category,
        run: async () => {
          await router.push({
            name: 'resources',
            params: { clusterId: clusterId.value, kind: kind.kind },
          })
        },
      })
    }
    list.push({
      id: 'reconnect',
      title: 'Reconnect this cluster',
      hint: 'Session',
      run: () => clusters.connect(clusterId.value),
    })
  }

  const cluster = clusters.clusters.find((c) => c.id === clusterId.value)
  if (cluster?.access.required) {
    list.push({
      id: 'access',
      title: 'Connect with Biebie Access',
      hint: 'Access',
      run: () => clusters.connectWithAccess(cluster),
    })
  }

  list.push(
    {
      id: 'forwards',
      title: 'Port forwards',
      hint: 'Go to',
      run: async () => {
        await router.push({ name: 'forwards' })
      },
    },
    {
      id: 'settings',
      title: 'Settings',
      hint: 'Go to',
      run: async () => {
        await router.push({ name: 'settings' })
      },
    },
  )

  return list
})

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return commands.value.slice(0, 12)
  return commands.value.filter((command) => command.title.toLowerCase().includes(needle)).slice(0, 12)
})

// Resource search runs against the cluster, so ⌘K finds objects by name and
// not only commands.
let searchTimer: number | undefined
watch(query, (value) => {
  window.clearTimeout(searchTimer)
  hits.value = []
  if (!connected.value || value.trim().length < 2) return

  searchTimer = window.setTimeout(async () => {
    try {
      hits.value = await api.search(
        clusterId.value,
        value.trim(),
        clusters.sessions[clusterId.value]?.namespace ?? '',
      )
    } catch (error) {
      ui.say(message(error), 'bad')
    }
  }, 200)
})

watch(
  () => ui.paletteOpen,
  async (open) => {
    if (!open) return
    query.value = ''
    highlighted.value = 0
    hits.value = []
    await nextTick()
    input.value?.focus()
  },
)

async function activate(index: number) {
  const command = filtered.value[index]
  if (!command) return
  ui.paletteOpen = false
  try {
    await command.run()
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}

async function openHit(hit: SearchHit) {
  ui.paletteOpen = false
  await router.push({
    name: 'resource',
    params: {
      clusterId: clusterId.value,
      kind: hit.kind,
      namespace: hit.namespace || '_',
      name: hit.name,
    },
  })
}

function move(delta: number) {
  const count = filtered.value.length
  if (!count) return
  highlighted.value = (highlighted.value + delta + count) % count
}
</script>

<template>
  <div
    v-if="ui.paletteOpen"
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-6 pt-24"
    @click.self="ui.paletteOpen = false"
  >
    <div class="w-full max-w-xl overflow-hidden rounded-2xl border border-line bg-surface-2 shadow-2xl">
      <input
        ref="input"
        v-model="query"
        class="w-full border-b border-line bg-transparent px-4 py-3.5 text-sm text-ink outline-none placeholder:text-ink-faint"
        placeholder="Search clusters, namespaces, resources…"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="activate(highlighted)"
        @keydown.esc="ui.paletteOpen = false"
      />

      <div class="max-h-96 overflow-y-auto p-1.5">
        <button
          v-for="(command, index) in filtered"
          :key="command.id"
          class="flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm"
          :class="index === highlighted ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-3'"
          @mouseenter="highlighted = index"
          @click="activate(index)"
        >
          <span class="truncate">{{ command.title }}</span>
          <span v-if="command.hint" class="shrink-0 text-[10px] uppercase tracking-widest text-ink-faint">
            {{ command.hint }}
          </span>
        </button>

        <div v-if="hits.length" class="mt-2 border-t border-line pt-2">
          <p class="px-3 pb-1 text-[10px] uppercase tracking-widest text-ink-faint">Resources</p>
          <button
            v-for="hit in hits"
            :key="`${hit.kind}/${hit.namespace}/${hit.name}`"
            class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-ink-muted hover:bg-surface-3"
            @click="openHit(hit)"
          >
            <StateDot :health="hit.health" />
            <span class="truncate text-ink">{{ hit.name }}</span>
            <span class="ml-auto shrink-0 text-[10px] uppercase tracking-widest text-ink-faint">
              {{ hit.kindTitle }}
            </span>
          </button>
        </div>

        <p v-if="!filtered.length && !hits.length" class="px-3 py-6 text-center text-sm text-ink-faint">
          Nothing matches “{{ query }}”.
        </p>
      </div>
    </div>
  </div>
</template>
