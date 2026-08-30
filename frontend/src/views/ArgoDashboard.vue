<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import ArgoActionDialog from '@/components/argocd/ArgoActionDialog.vue'
import StateDot from '@/components/common/StateDot.vue'
import { api, message, openInBrowser } from '@/api'
import { age } from '@/composables/format'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'
import { ArgoActivityKind, Health } from '@/types'
import type { ArgoApp, ArgoAppRef, ArgoCard, ArgoDashboard } from '@/types'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const router = useRouter()
const ui = useUIStore()

const dashboard = ref<ArgoDashboard | null>(null)
const loading = ref(true)
const opening = ref(false)
const error = ref('')

const filter = ref('')
const action = ref<{ mode: 'sync' | 'refresh'; only?: ArgoAppRef } | null>(null)

const cluster = computed(() => clusters.clusters.find((entry) => entry.id === props.clusterId))
const summary = computed(() => dashboard.value?.summary)

/**
 * The timeline filter searches what it was given rather than asking the
 * cluster again: the entries are already here, and a round trip per keystroke
 * would make a panel that exists to be skimmed feel like a query.
 */
const activity = computed(() => {
  const entries = dashboard.value?.activity ?? []
  const needle = filter.value.trim().toLowerCase()
  if (!needle) return entries
  return entries.filter((entry) =>
    `${entry.reason} ${entry.object} ${entry.message} ${entry.namespace}`
      .toLowerCase()
      .includes(needle),
  )
})

/**
 * Partial for the same reason every other presentation map here is: the enum
 * carries a `$zero` member for Go's zero value, which is not a category the
 * timeline has anything to say about.
 */
const activityHealths: Partial<Record<ArgoActivityKind, Health>> = {
  [ArgoActivityKind.ArgoActivityFailure]: Health.HealthCritical,
  [ArgoActivityKind.ArgoActivityProgress]: Health.HealthProgress,
  [ArgoActivityKind.ArgoActivitySuccess]: Health.HealthHealthy,
  [ArgoActivityKind.ArgoActivityInfo]: Health.HealthUnknown,
}

function activityHealth(category: ArgoActivityKind): Health {
  return activityHealths[category] ?? Health.HealthUnknown
}

/** The health strip reads counts, so a zero is worth showing plainly. */
const strip = computed(() => [
  { label: 'Applications', value: summary.value?.applications ?? 0, tone: 'text-ink' },
  { label: 'Synced', value: summary.value?.synced ?? 0, tone: 'text-ok' },
  { label: 'Healthy', value: summary.value?.healthy ?? 0, tone: 'text-ok' },
  { label: 'Out of sync', value: summary.value?.outOfSync ?? 0, tone: 'text-warn' },
  { label: 'Degraded', value: summary.value?.degraded ?? 0, tone: 'text-bad' },
  { label: 'Missing', value: summary.value?.missing ?? 0, tone: 'text-warn' },
  { label: 'Progressing', value: summary.value?.progressing ?? 0, tone: 'text-info' },
])

async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await api.argoDashboard(props.clusterId)
  } catch (err) {
    error.value = message(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.clusterId, load)

function openCard(card: ArgoCard) {
  if (!card.kind) return
  void router.push({ name: 'resources', params: { clusterId: props.clusterId, kind: card.kind } })
}

function openApp(app: ArgoApp) {
  void router.push({
    name: 'resource',
    params: {
      clusterId: props.clusterId,
      kind: 'applications.argoproj.io',
      namespace: app.namespace,
      name: app.name,
    },
  })
}

/**
 * The Argo CD UI is reached through the port forward the panel already knows
 * about, so the browser opens on a loopback URL rather than on whatever
 * ingress a customer may or may not have published.
 */
async function openUI() {
  opening.value = true
  try {
    const endpoint = await api.openArgoUI(props.clusterId)
    openInBrowser(endpoint.url)
    ui.say(
      endpoint.reused
        ? `Opened the Argo CD UI on the port forward already running at ${endpoint.url}.`
        : `Forwarded the Argo CD server to ${endpoint.url}. It redirects to HTTPS, so your browser shows one certificate warning.`,
    )
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    opening.value = false
  }
}
</script>

<template>
  <div class="h-full overflow-y-auto px-6 py-6">
    <div class="flex items-center justify-between gap-3">
      <h1 class="text-sm font-semibold uppercase tracking-widest text-ink-faint">Argo CD</h1>
      <div class="flex items-center gap-2">
        <button
          v-if="dashboard?.installed"
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-50"
          :disabled="opening"
          @click="openUI"
        >
          {{ opening ? 'Forwarding…' : 'Open Argo CD UI' }}
        </button>
        <button
          class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
          @click="load"
        >
          Refresh
        </button>
      </div>
    </div>

    <p v-if="error" class="mt-4 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
      {{ error }}
    </p>

    <p v-else-if="loading" class="mt-6 text-sm text-ink-muted">Reading Argo CD state…</p>

    <p
      v-else-if="!dashboard?.installed"
      class="mt-6 rounded-xl border border-line bg-surface-2 px-4 py-6 text-sm text-ink-muted"
    >
      This cluster serves no Argo CD Application definition, so there is nothing to show. Install
      Argo CD and reconnect, and this page fills itself in.
    </p>

    <template v-else>
      <div class="mt-4 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-line bg-line sm:grid-cols-4 lg:grid-cols-7">
        <div v-for="entry in strip" :key="entry.label" class="bg-surface-2 px-4 py-3">
          <p class="truncate text-[11px] text-ink-faint">{{ entry.label }}</p>
          <p class="mt-0.5 font-mono text-xl" :class="entry.tone">{{ entry.value }}</p>
        </div>
      </div>

      <p v-if="dashboard.namespace" class="mt-2 text-xs text-ink-faint">
        argocd-server runs in
        <span class="font-mono text-ink-muted">{{ dashboard.namespace }}</span>
      </p>

      <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="card in dashboard.cards ?? []"
          :key="card.title"
          class="rounded-xl border border-line bg-surface-2 p-4"
          :class="card.kind ? 'cursor-pointer hover:border-line-strong' : ''"
          @click="openCard(card)"
        >
          <div class="flex items-baseline justify-between gap-2">
            <p class="truncate text-xs text-ink-faint">{{ card.title }}</p>
            <span v-if="card.kind" class="text-[11px] text-ink-faint">View →</span>
          </div>

          <p class="mt-1 font-mono text-2xl text-ink">
            <template v-if="card.leads">
              {{ card.healthy }}<span class="text-ink-faint">/{{ card.total }}</span>
            </template>
            <template v-else>{{ card.total }}</template>
          </p>

          <div v-if="card.chips?.length" class="mt-2 flex flex-wrap gap-1.5">
            <span
              v-for="chip in card.chips"
              :key="chip.label"
              class="flex items-center gap-1.5 rounded-full border border-line px-2 py-0.5 text-[11px] text-ink-muted"
            >
              <StateDot :health="chip.health" />
              {{ chip.count }} {{ chip.label.toLowerCase() }}
            </span>
          </div>

          <div v-if="card.leads" class="mt-3 flex gap-2" @click.stop>
            <button
              class="rounded-lg bg-brand px-2.5 py-1 text-xs font-semibold text-surface-1 hover:bg-brand-strong"
              @click="action = { mode: 'sync' }"
            >
              Sync
            </button>
            <button
              class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
              @click="action = { mode: 'refresh' }"
            >
              Refresh
            </button>
          </div>
        </div>
      </div>

      <section class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
          Needs attention
        </h2>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <p
            v-if="!dashboard.needsAttention?.length"
            class="bg-surface-2 px-4 py-6 text-center text-sm text-ink-muted"
          >
            Every application is synced and healthy.
          </p>
          <ul v-else class="divide-y divide-line">
            <li
              v-for="app in dashboard.needsAttention"
              :key="`${app.namespace}/${app.name}`"
              class="flex cursor-pointer items-center gap-3 bg-surface-2 px-4 py-2.5 hover:bg-surface-3"
              @click="openApp(app)"
            >
              <StateDot :health="app.health" />
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm text-ink">
                  {{ app.name }}
                  <span class="text-ink-faint"> · {{ app.namespace }}</span>
                </p>
                <p class="mt-0.5 truncate text-xs text-ink-muted">{{ app.reason }}</p>
              </div>
              <span class="hidden shrink-0 text-xs text-ink-faint sm:block">
                {{ app.sync || '—' }} · {{ app.healthStatus || '—' }}
              </span>
              <div class="flex shrink-0 gap-1.5" @click.stop>
                <button
                  class="rounded-md border border-line px-2 py-0.5 text-[11px] text-ink-muted hover:text-ink"
                  @click="action = { mode: 'sync', only: { namespace: app.namespace, name: app.name } }"
                >
                  Sync
                </button>
                <button
                  class="rounded-md border border-line px-2 py-0.5 text-[11px] text-ink-muted hover:text-ink"
                  @click="action = { mode: 'refresh', only: { namespace: app.namespace, name: app.name } }"
                >
                  Refresh
                </button>
              </div>
            </li>
          </ul>
        </div>
      </section>

      <section class="mt-6 pb-4">
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
            Recent activity
          </h2>
          <input
            v-model="filter"
            class="w-64 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-sm text-ink outline-none focus:border-brand"
            placeholder="Filter activity"
            spellcheck="false"
          />
        </div>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <p
            v-if="!activity.length"
            class="bg-surface-2 px-4 py-6 text-center text-sm text-ink-muted"
          >
            <!-- Events age out on the cluster's own schedule, so an empty
                 timeline is a quiet hour rather than a missing feature. -->
            {{
              dashboard.activity?.length
                ? 'Nothing matches that filter.'
                : 'No recent Argo CD events. Kubernetes keeps them for about an hour.'
            }}
          </p>
          <ul v-else class="divide-y divide-line">
            <li
              v-for="entry in activity"
              :key="entry.uid"
              class="flex items-start gap-3 bg-surface-2 px-4 py-2.5"
            >
              <StateDot :health="activityHealth(entry.category)" class="mt-1.5" />
              <div class="min-w-0 flex-1">
                <p class="text-sm text-ink">
                  <span class="font-medium">{{ entry.reason }}</span>
                  <span class="text-ink-faint"> · {{ entry.object }}</span>
                </p>
                <p class="mt-0.5 truncate text-xs text-ink-muted">{{ entry.message }}</p>
              </div>
              <span class="shrink-0 font-mono text-xs text-ink-faint">{{ age(entry.at) }}</span>
            </li>
          </ul>
        </div>
      </section>
    </template>

    <ArgoActionDialog
      v-if="action"
      open
      :mode="action.mode"
      :cluster-id="clusterId"
      :cluster="cluster"
      :only="action.only"
      @close="action = null"
      @done="load"
    />
  </div>
</template>
