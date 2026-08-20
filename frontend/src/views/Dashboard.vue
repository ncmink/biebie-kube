<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import ClusterCard from '@/components/cluster/ClusterCard.vue'
import ClusterDialog from '@/components/cluster/ClusterDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ContextMenu from '@/components/common/ContextMenu.vue'
import { message } from '@/api'
import type { ContextMenuItem } from '@/composables/menu'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import { ClusterState } from '@/types'
import type { Cluster } from '@/types'

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()
const adding = ref(false)
const editing = ref<Cluster | null>(null)
const removing = ref<Cluster | null>(null)
const menu = ref<{ x: number; y: number; cluster: Cluster } | null>(null)
const groupError = ref('')

onMounted(async () => {
  await clusters.load()
  for (const cluster of clusters.clusters) {
    if (cluster.access.required && cluster.access.profileId) {
      void clusters.refreshAccess(cluster.access.profileId)
    }
  }
})

async function open(clusterId: string) {
  await clusters.open(clusterId)
  await router.push({ name: 'overview', params: { clusterId } })
}

async function setGroupHidden(key: string, hidden: boolean) {
  groupError.value = ''
  try {
    await clusters.setGroupHidden(key, hidden)
  } catch (err) {
    groupError.value = message(err)
  }
}

function openMenu(cluster: Cluster, event: MouseEvent) {
  menu.value = { x: event.clientX, y: event.clientY, cluster }
}

/**
 * The right-click menu offers what the card offers, plus the filing actions
 * that would otherwise mean opening the form to change one field.
 */
const menuItems = computed<ContextMenuItem[]>(() => {
  const cluster = menu.value?.cluster
  if (!cluster) return []

  const state = clusters.sessions[cluster.id]?.state
  const connected = state === ClusterState.ClusterConnected
  const customer = cluster.customerName || cluster.customerId

  const items: ContextMenuItem[] = [
    { id: 'open', label: connected ? 'Open cluster' : 'Connect and open' },
  ]
  if (connected) {
    items.push({ id: 'disconnect', label: 'Disconnect' })
  }
  if (cluster.access.required) {
    items.push({ id: 'access', label: 'Connect with Biebie Access' })
  }

  items.push({ id: 'edit', label: 'Edit cluster…', divider: true })
  items.push({
    id: 'archive',
    label: cluster.archived ? 'Take out of the archive' : 'Archive (hidden)',
  })

  if (customer) {
    items.push({
      id: 'customer',
      label: customerHiddenFor(cluster)
        ? `Show “${customer}” on the list`
        : `Hide customer “${customer}”`,
      divider: true,
    })
  }
  items.push({ id: 'remove', label: 'Remove from Biebie Kube…', danger: true, divider: true })
  return items
})

function runMenuAction(id: string) {
  const cluster = menu.value?.cluster
  menu.value = null
  if (!cluster) return

  switch (id) {
    case 'open':
      void open(cluster.id)
      break
    case 'disconnect':
      void clusters.disconnect(cluster.id)
      break
    case 'access':
      void connectAccess(cluster)
      break
    case 'edit':
      editing.value = cluster
      break
    case 'archive':
      void setArchived(cluster, !cluster.archived)
      break
    case 'customer':
      void setGroupHidden(cluster.customerId || cluster.customerName, !customerHiddenFor(cluster))
      break
    case 'remove':
      removing.value = cluster
      break
  }
}

function customerHiddenFor(cluster: Cluster) {
  const key = cluster.customerId || cluster.customerName
  return Boolean(clusters.groups.find((group) => group.key === key)?.hidden)
}

/**
 * Archiving makes the card disappear on purpose, so say where it went rather
 * than leaving the engineer wondering what just happened.
 */
async function setArchived(cluster: Cluster, archived: boolean) {
  groupError.value = ''
  try {
    await clusters.setArchived(cluster.id, archived)
    if (archived && !clusters.showHidden) {
      ui.say(`${cluster.name} is in the archive, which is hidden. Press Show hidden to see it.`)
    }
  } catch (err) {
    groupError.value = message(err)
  }
}

async function connectAccess(cluster: Cluster) {
  try {
    await clusters.connectWithAccess(cluster)
    ui.say('Asked Biebie Access to connect this customer. Biebie Kube will retry on its own.')
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}

async function remove() {
  const cluster = removing.value
  removing.value = null
  if (!cluster) return
  try {
    await clusters.remove(cluster.id)
    ui.say(`Removed ${cluster.name}. Your kubeconfig is untouched.`)
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <main class="h-full overflow-y-auto">
    <div class="mx-auto max-w-6xl px-8 py-8">
      <div class="flex items-end justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold tracking-tight">Clusters</h1>
          <p class="mt-1 text-sm text-ink-muted">
            Grouped by customer. Biebie Access provides the network; this application provides
            Kubernetes.
          </p>
        </div>
        <div class="flex items-center gap-2">
          <button
            v-if="clusters.hiddenCount"
            class="rounded-lg border border-line px-3 py-2 text-sm hover:text-ink"
            :class="clusters.showHidden ? 'text-ink' : 'text-ink-muted'"
            :title="
              clusters.showHidden
                ? 'Put the hidden customers away again'
                : 'Reveal the customers kept off this list'
            "
            @click="clusters.toggleShowHidden()"
          >
            {{ clusters.showHidden ? 'Hide' : 'Show' }} hidden ({{ clusters.hiddenCount }})
          </button>
          <button
            class="rounded-lg bg-brand px-3 py-2 text-sm font-semibold text-surface-1 hover:bg-brand-strong"
            @click="adding = true"
          >
            Add cluster
          </button>
        </div>
      </div>

      <p v-if="clusters.error" class="mt-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
        {{ clusters.error }}
      </p>
      <p v-if="groupError" class="mt-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
        {{ groupError }}
      </p>

      <div v-if="!clusters.clusters.length" class="mt-10 rounded-2xl border border-dashed border-line px-8 py-14 text-center">
        <p class="text-sm font-medium text-ink">No clusters yet</p>
        <p class="mx-auto mt-2 max-w-md text-sm leading-relaxed text-ink-muted">
          Import a kubeconfig and pick the contexts you work with. Biebie Kube reads your kubeconfig
          and never writes to it.
        </p>
        <button
          class="mt-5 rounded-lg bg-brand px-3 py-2 text-sm font-semibold text-surface-1 hover:bg-brand-strong"
          @click="adding = true"
        >
          Add your first cluster
        </button>
      </div>

      <div
        v-else-if="!clusters.visibleCount"
        class="mt-10 rounded-2xl border border-dashed border-line px-8 py-12 text-center"
      >
        <p class="text-sm font-medium text-ink">Everything is put away</p>
        <p class="mx-auto mt-2 max-w-md text-sm leading-relaxed text-ink-muted">
          Nothing was deleted. Every cluster is either under a hidden customer or in the archive.
        </p>
        <button
          class="mt-5 rounded-lg bg-brand px-3 py-2 text-sm font-semibold text-surface-1 hover:bg-brand-strong"
          @click="clusters.setShowHidden(true)"
        >
          Show {{ clusters.hiddenCount }} hidden
        </button>
      </div>

      <section v-for="group in clusters.byCustomer" :key="group.key" class="mt-8">
        <div class="flex items-center gap-3">
          <h2
            class="text-xs font-semibold uppercase tracking-widest"
            :class="group.hidden ? 'text-ink-faint/60' : 'text-ink-faint'"
          >
            {{ group.customer }}
          </h2>
          <span class="text-xs text-ink-faint/70">{{ group.items.length }}</span>
          <span
            v-if="group.hidden"
            class="rounded-full border border-line px-2 py-0.5 text-[10px] uppercase tracking-wide text-ink-faint"
          >
            Hidden
          </span>
          <span class="h-px flex-1 bg-line" />
          <button
            v-if="group.hideable"
            class="rounded-lg px-2 py-1 text-xs text-ink-faint hover:bg-surface-2 hover:text-ink"
            :title="
              group.hidden
                ? 'Show this customer on the cluster list'
                : 'Keep this customer off the cluster list'
            "
            @click="setGroupHidden(group.key, !group.hidden)"
          >
            {{ group.hidden ? 'Show' : 'Hide' }}
          </button>
        </div>
        <div class="mt-3 grid gap-3 md:grid-cols-2" :class="group.hidden ? 'opacity-60' : ''">
          <ClusterCard
            v-for="cluster in group.items"
            :key="cluster.id"
            :cluster="cluster"
            @open="open(cluster.id)"
            @menu="openMenu(cluster, $event)"
          />
        </div>
      </section>
    </div>

    <ContextMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="menuItems"
      @close="menu = null"
      @select="runMenuAction"
    />

    <ClusterDialog :open="adding" @close="adding = false" @saved="clusters.load()" />
    <ClusterDialog
      :open="Boolean(editing)"
      :cluster="editing"
      @close="editing = null"
      @saved="clusters.load()"
    />

    <ConfirmDialog
      :open="Boolean(removing)"
      title="Remove this cluster from Biebie Kube?"
      detail="Only Biebie's record is removed. Your kubeconfig is left exactly as it is."
      :cluster="removing ?? undefined"
      confirm-label="Remove"
      @cancel="removing = null"
      @confirm="remove"
    />
  </main>
</template>
