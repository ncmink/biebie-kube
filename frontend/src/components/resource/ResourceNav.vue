<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { api, events, on } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { usePortForwardStore } from '@/stores/sessions'
import type { KindInfo } from '@/types'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const forwards = usePortForwardStore()
const route = useRoute()

const openKey = 'biebie-kube.nav-groups'
const open = ref<Record<string, boolean>>(readOpen())
const presence = ref<Record<string, number>>({})

type NavItem = { type: 'item'; kind: KindInfo }
type NavGroup = { type: 'group'; category: string; kinds: KindInfo[] }
type NavEntry = NavItem | NavGroup

const catalogue = computed(() => clusters.catalogues[props.clusterId] ?? [])
const namespace = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')
const connected = computed(() => clusters.sessions[props.clusterId]?.state === 'connected')
const activeKind = computed(() => String(route.params.kind ?? ''))
const forwardCount = computed(
  () => forwards.forwards.filter((session) => session.clusterId === props.clusterId).length,
)

const tree = computed<NavEntry[]>(() => {
  const entries: NavEntry[] = []
  let group: NavGroup | undefined

  const flush = () => {
    if (group) entries.push(group)
    group = undefined
  }

  for (const kind of catalogue.value) {
    if (kind.standalone) {
      flush()
      entries.push({ type: 'item', kind })
      continue
    }
    if (!group || group.category !== kind.category) {
      flush()
      group = { type: 'group', category: kind.category, kinds: [kind] }
      continue
    }
    group.kinds.push(kind)
  }
  flush()
  return entries
})

function readOpen(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(openKey)
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {}
  } catch {
    return {}
  }
}

function isOpen(category: string): boolean {
  return open.value[category] !== false
}

function toggle(category: string) {
  open.value = { ...open.value, [category]: !isOpen(category) }
  localStorage.setItem(openKey, JSON.stringify(open.value))
}

/** empty is true only when the cluster answered with a zero; unknown stays bright. */
function empty(kind: string): boolean {
  return presence.value[kind] === 0
}

function linkClass(kind: string): string {
  if (activeKind.value === kind) return 'bg-brand/15 text-ink'
  if (empty(kind)) return 'text-ink-faint/50 hover:bg-surface-2 hover:text-ink-faint'
  return 'text-ink-muted hover:bg-surface-2 hover:text-ink'
}

async function refreshCounts() {
  if (!connected.value) {
    presence.value = {}
    return
  }
  try {
    const rows = await api.countResources(props.clusterId, namespace.value)
    const next: Record<string, number> = {}
    for (const row of rows) next[row.kind] = row.count
    presence.value = next
  } catch {
    presence.value = {}
  }
}

watch(activeKind, (kind) => {
  const info = catalogue.value.find((entry) => entry.kind === kind)
  if (info && !info.standalone) {
    open.value = { ...open.value, [info.category]: true }
  }
})

onMounted(refreshCounts)
watch([() => props.clusterId, namespace, connected], refreshCounts)

let stopWatch: (() => void) | undefined
onMounted(() => {
  stopWatch = on(events.resources, (event) => {
    if (event.clusterId === props.clusterId) void refreshCounts()
  })
})
onUnmounted(() => stopWatch?.())
</script>

<template>
  <nav class="overflow-y-auto px-1.5 py-2 text-[13px]">
    <RouterLink
      :to="{ name: 'overview', params: { clusterId } }"
      class="flex items-center gap-2 rounded-md px-2 py-1.5"
      :class="route.name === 'overview' ? 'bg-brand/15 text-ink' : 'text-ink-muted hover:bg-surface-2 hover:text-ink'"
    >
      <svg viewBox="0 0 16 16" class="size-3.5 shrink-0" fill="none" aria-hidden="true">
        <rect x="1.5" y="1.5" width="5.5" height="5.5" rx="1" stroke="currentColor" stroke-width="1.2" />
        <rect x="9" y="1.5" width="5.5" height="5.5" rx="1" stroke="currentColor" stroke-width="1.2" />
        <rect x="1.5" y="9" width="5.5" height="5.5" rx="1" stroke="currentColor" stroke-width="1.2" />
        <rect x="9" y="9" width="5.5" height="5.5" rx="1" stroke="currentColor" stroke-width="1.2" />
      </svg>
      Overview
    </RouterLink>

    <template v-for="entry in tree" :key="entry.type === 'item' ? entry.kind.kind : entry.category">
      <RouterLink
        v-if="entry.type === 'item'"
        :to="{ name: 'resources', params: { clusterId, kind: entry.kind.kind } }"
        class="mt-0.5 flex items-center gap-2 rounded-md px-2 py-1.5"
        :class="linkClass(entry.kind.kind)"
      >
        <svg
          v-if="entry.kind.kind === 'nodes'"
          viewBox="0 0 16 16"
          class="size-3.5 shrink-0"
          fill="none"
          aria-hidden="true"
        >
          <rect x="2" y="2" width="12" height="4" rx="1" stroke="currentColor" stroke-width="1.2" />
          <rect x="2" y="7" width="12" height="4" rx="1" stroke="currentColor" stroke-width="1.2" />
          <path d="M4 13.5h8" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
        </svg>
        <svg
          v-else-if="entry.kind.kind === 'namespaces'"
          viewBox="0 0 16 16"
          class="size-3.5 shrink-0"
          fill="none"
          aria-hidden="true"
        >
          <path d="M8 2.5l5 5-5 5-5-5 5-5z" stroke="currentColor" stroke-width="1.2" />
        </svg>
        <svg
          v-else-if="entry.kind.kind === 'events'"
          viewBox="0 0 16 16"
          class="size-3.5 shrink-0"
          fill="none"
          aria-hidden="true"
        >
          <circle cx="8" cy="8" r="5.5" stroke="currentColor" stroke-width="1.2" />
          <path d="M8 5v3.2L10 10" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
        </svg>
        <span class="truncate">{{ entry.kind.title }}</span>
      </RouterLink>

      <div v-else class="mt-1">
        <button
          class="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-ink-muted hover:bg-surface-2 hover:text-ink"
          @click="toggle(entry.category)"
        >
          <svg
            viewBox="0 0 16 16"
            class="size-3 shrink-0 text-ink-faint transition-transform"
            :class="isOpen(entry.category) ? 'rotate-90' : ''"
            fill="none"
            aria-hidden="true"
          >
            <path d="M6 4l5 4-5 4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
          </svg>
          <svg viewBox="0 0 16 16" class="size-3.5 shrink-0" fill="none" aria-hidden="true">
            <path
              v-if="entry.category === 'Workloads'"
              d="M8 2.5l5 3v5l-5 3-5-3v-5l5-3zM3 5.5l5 3 5-3M8 8.5v5"
              stroke="currentColor"
              stroke-width="1.2"
            />
            <g v-else-if="entry.category === 'Config'">
              <path
                d="M6.2 2.8h3.6l.7 1.6 1.7.7.7 1.7-1.2 1.4 1.2 1.4-.7 1.7-1.7.7-.7 1.6H6.2l-.7-1.6-1.7-.7-.7-1.7 1.2-1.4-1.2-1.4.7-1.7 1.7-.7.7-1.6z"
                stroke="currentColor"
                stroke-width="1.2"
              />
              <circle cx="8" cy="8" r="1.6" stroke="currentColor" stroke-width="1.2" />
            </g>
            <path
              v-else-if="entry.category === 'Network'"
              d="M5 4.5h6M8 4.5v7M5 11.5h6M3.5 7h3M9.5 9h3"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
            <path
              v-else-if="entry.category === 'Storage'"
              d="M3 5c0-1.2 2.2-2.2 5-2.2s5 1 5 2.2v6c0 1.2-2.2 2.2-5 2.2s-5-1-5-2.2V5zM3 5c0 1.2 2.2 2.2 5 2.2s5-1 5-2.2"
              stroke="currentColor"
              stroke-width="1.2"
            />
            <path
              v-else
              d="M8 2.8l4.5 2v3.4c0 2.6-1.8 4.4-4.5 5.3-2.7-.9-4.5-2.7-4.5-5.3V4.8l4.5-2z"
              stroke="currentColor"
              stroke-width="1.2"
            />
          </svg>
          <span class="truncate font-medium">{{ entry.category }}</span>
        </button>

        <div v-if="isOpen(entry.category)" class="ml-3 border-l border-line/80 py-0.5 pl-2">
          <RouterLink
            v-for="kind in entry.kinds"
            :key="kind.kind"
            :to="{ name: 'resources', params: { clusterId, kind: kind.kind } }"
            class="mt-px block truncate rounded-md px-2 py-1"
            :class="linkClass(kind.kind)"
          >
            {{ kind.title }}
          </RouterLink>
          <RouterLink
            v-if="entry.category === 'Network'"
            :to="{ name: 'forwards' }"
            class="mt-px block truncate rounded-md px-2 py-1"
            :class="
              route.name === 'forwards'
                ? 'bg-brand/15 text-ink'
                : forwardCount
                  ? 'text-ink-muted hover:bg-surface-2 hover:text-ink'
                  : 'text-ink-faint/50 hover:bg-surface-2 hover:text-ink-faint'
            "
          >
            Port Forwarding
          </RouterLink>
        </div>
      </div>
    </template>

    <p v-if="!tree.length" class="px-2 py-4 text-xs text-ink-faint">
      Connect to load this cluster's resource types.
    </p>
  </nav>
</template>
