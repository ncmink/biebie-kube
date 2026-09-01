<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ContextMenu from '@/components/common/ContextMenu.vue'
import CreateResourceDialog from '@/components/resource/CreateResourceDialog.vue'
import ResourceActionDialog from '@/components/resource/ResourceActionDialog.vue'
import ResourceDrawer from '@/components/resource/ResourceDrawer.vue'
import ResourceTable from '@/components/resource/ResourceTable.vue'
import { api, message } from '@/api'
import { actionsFor, menuItems } from '@/composables/actions'
import type { ActionDescriptor } from '@/composables/actions'
import { asKind, singularTitle } from '@/composables/kind'
import type { ContextMenuItem } from '@/composables/menu'
import { useClusterStore } from '@/stores/clusters'
import { useResourceStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { EnvironmentKind } from '@/types'
import type { ResourceRow } from '@/types'

const props = defineProps<{ clusterId: string; kind: string }>()

const clusters = useClusterStore()
const resources = useResourceStore()
const ui = useUIStore()

const namespace = computed(() => clusters.sessions[props.clusterId]?.namespace ?? '')
const kindInfo = computed(() =>
  (clusters.catalogues[props.clusterId] ?? []).find((entry) => entry.kind === props.kind),
)
const cluster = computed(() => clusters.clusters.find((entry) => entry.id === props.clusterId))
const resourceKind = computed(() => asKind(props.kind, clusters.catalogues[props.clusterId]))
const heading = computed(() => singularTitle(kindInfo.value?.title ?? props.kind))
const selected = ref<ResourceRow | null>(null)

const identity = computed(() => `${props.clusterId}/${props.kind}/${namespace.value}`)

/**
 * count is what the table is actually showing, against what exists.
 *
 * Reporting the loaded row count alone is how a list of twelve thousand pods
 * came to look like a list of two thousand: the number on screen matched the
 * rows on screen, and both were wrong about the cluster.
 */
const count = computed(() => {
  const shown = resources.rows.length
  const { matched, total, syncing } = resources
  const suffix = syncing ? '+' : ''

  if (resources.filter.trim()) return `${matched}${suffix} of ${total}${suffix} match`
  if (shown < matched) return `${shown} of ${matched}${suffix}`
  return `${matched}${suffix}`
})

/**
 * The menu belongs to the page rather than to the table, because the drawer
 * opens the same one for the row it is showing. Two menus built from two
 * copies of the same list is how a row's actions and the inspector's come to
 * disagree about what a kind offers.
 */
const menu = ref<{ row: ResourceRow; x: number; y: number } | null>(null)
const acting = ref<{ row: ResourceRow; action: ActionDescriptor } | null>(null)
const deleting = ref<ResourceRow | null>(null)

/**
 * Creating is offered from the list rather than from a kind, and the dialog
 * asks the backend whether it is allowed rather than being hidden here.
 *
 * Whether a namespace is somebody's GitOps destination is a question with a
 * cluster in the answer, and asking it on every list render would be a round
 * trip per navigation for a button most people will not press.
 */
const creating = ref(false)

function reload() {
  void resources.load(props.clusterId, props.kind, namespace.value)
}

onMounted(reload)
watch(identity, () => {
  selected.value = null
  menu.value = null
  resources.reset()
  reload()
})

function open(row: ResourceRow) {
  selected.value = row
}

const offered = computed(() => (menu.value ? actionsFor(kindInfo.value, menu.value.row) : []))

const items = computed<ContextMenuItem[]>(() => [
  ...menuItems(offered.value),
  { id: 'delete', label: 'Delete…', danger: true, divider: offered.value.length > 0 },
])

function openMenu(row: ResourceRow, event: MouseEvent) {
  menu.value = { row, x: event.clientX, y: event.clientY }
}

function choose(id: string) {
  // Both are read before the menu closes: the list of actions is derived from
  // the row the menu was opened on, and clearing it first would leave nothing
  // to look the chosen action up in.
  const opened = menu.value
  const actions = offered.value
  menu.value = null
  if (!opened) return

  if (id === 'delete') {
    deleting.value = opened.row
    return
  }
  const action = actions.find((entry) => entry.action === id)
  if (action) acting.value = { row: opened.row, action }
}

async function remove() {
  const row = deleting.value
  deleting.value = null
  if (!row || !resourceKind.value) return

  try {
    await api.deleteResource(props.clusterId, {
      kind: resourceKind.value,
      namespace: row.namespace,
      name: row.name,
    })
    ui.say(`Deleted ${row.name}.`)
    if (selected.value?.key === row.key) selected.value = null
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <header class="flex shrink-0 items-center gap-3 border-b border-line px-6 py-3">
      <h1 class="text-sm font-semibold text-ink">{{ kindInfo?.title ?? kind }}</h1>
      <span class="font-mono text-xs text-ink-faint">{{ count }}</span>
      <span v-if="resources.syncing" class="text-xs text-ink-faint">syncing…</span>
      <input
        :value="resources.filter"
        class="ml-auto w-64 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
        placeholder="Filter by name"
        spellcheck="false"
        @input="resources.setFilter(($event.target as HTMLInputElement).value)"
      />
      <button
        class="rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="reload"
      >
        Refresh
      </button>
      <button
        class="rounded-lg border border-line px-2.5 py-1.5 text-xs text-ink-muted hover:text-ink"
        @click="creating = true"
      >
        Create {{ heading }}…
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
          v-else
          :identity="identity"
          :rows="resources.rows"
          :columns="resources.columns"
          :namespaced="resources.namespaced"
          :sort-key="resources.sortKey"
          :sort-desc="resources.sortDesc"
          :selected="selected"
          @open="open"
          @menu="openMenu"
          @sort="resources.sortBy"
          @end="resources.more"
        />
      </div>
      <ResourceDrawer
        v-if="selected"
        :cluster-id="clusterId"
        :kind="kind"
        :row="selected"
        :kind-title="kindInfo?.title ?? kind"
        @menu="openMenu(selected, $event)"
        @delete="deleting = selected"
        @close="selected = null"
      />
    </div>

    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="items"
      @select="choose"
      @close="menu = null"
    />

    <ResourceActionDialog
      v-if="acting && resourceKind"
      :cluster-id="clusterId"
      :kind="resourceKind"
      :kind-title="kindInfo?.title ?? kind"
      :row="acting.row"
      :action="acting.action"
      :cluster="cluster"
      @close="acting = null"
    />

    <CreateResourceDialog
      v-if="creating"
      :cluster-id="clusterId"
      :kind="kind"
      :kind-title="kindInfo?.title ?? kind"
      :namespace="namespace"
      :cluster="cluster"
      @created="reload"
      @close="creating = false"
    />

    <ConfirmDialog
      :open="!!deleting"
      :title="`Delete ${heading} “${deleting?.name}”?`"
      :detail="
        deleting?.namespace
          ? `In namespace ${deleting.namespace}. This cannot be undone.`
          : 'This cannot be undone.'
      "
      :cluster="cluster"
      :require-typing="
        cluster?.environmentKind === EnvironmentKind.EnvironmentProduction
          ? deleting?.name
          : undefined
      "
      @cancel="deleting = null"
      @confirm="remove"
    />
  </div>
</template>
