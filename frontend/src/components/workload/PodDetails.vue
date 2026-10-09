<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { api, message, openInBrowser } from '@/api'
import { agoClock } from '@/composables/format'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'
import { PortForwardState } from '@/types'
import type {
  ContainerInfo,
  ContainerPort,
  ContainerProbe,
  EnvVar,
  PodDetail,
  PortForwardSession,
  VolumeMount,
} from '@/types'

const props = defineProps<{
  clusterId: string
  namespace: string
  name: string
  /** Bumped by the list Refresh so this inspector re-reads the live pod. */
  revision?: number
}>()

const forwards = usePortForwardStore()
const ui = useUIStore()

const detail = ref<PodDetail | null>(null)
const error = ref('')
const busy = ref<Record<number, boolean>>({})
const open = ref<Record<string, boolean>>({})

async function load(opts?: { quiet?: boolean }) {
  const quiet = opts?.quiet && detail.value != null
  if (!quiet) {
    error.value = ''
    open.value = {}
  }
  try {
    detail.value = await api.podDetail(props.clusterId, props.namespace, props.name)
  } catch (err) {
    if (!quiet) {
      detail.value = null
      error.value = message(err)
    }
  }
}

onMounted(() => void load())

watch(
  () => [props.clusterId, props.namespace, props.name],
  () => void load(),
)

watch(
  () => props.revision,
  (tick) => {
    if (!tick) return
    void load({ quiet: true })
  },
)

function toggle(key: string) {
  open.value = { ...open.value, [key]: !open.value[key] }
}

function shown(key: string): boolean {
  return open.value[key] === true
}

function pairs(record: { [_ in string]?: string } | null | undefined): [string, string][] {
  return Object.entries(record ?? {})
    .filter((entry): entry is [string, string] => entry[1] != null)
    .sort(([a], [b]) => a.localeCompare(b))
}

const labelPairs = computed(() => pairs(detail.value?.labels))
const annotationPairs = computed(() => pairs(detail.value?.annotations))

const volumeGroups = computed(() => {
  const groups = new Map<string, string[]>()
  for (const volume of detail.value?.volumes ?? []) {
    groups.set(volume.type, [...(groups.get(volume.type) ?? []), volume.name])
  }
  return [...groups.entries()].map(([type, names]) => ({ type, names }))
})

const sections = computed(() =>
  [
    { title: 'Init Containers', items: detail.value?.initContainers ?? [] },
    { title: 'Containers', items: detail.value?.containers ?? [] },
  ].filter((section) => section.items.length > 0),
)

function count(n: number, singular: string): string {
  return `${n} ${n === 1 ? singular : singular + 's'}`
}

function clock(iso: string | null | undefined): string {
  if (!iso) return ''
  const then = new Date(iso)
  return Number.isNaN(then.getTime()) ? '' : then.toLocaleString()
}

function statusLine(item: ContainerInfo): string {
  const state = item.state || '—'
  return item.ready ? `${state}, ready` : state
}

function lastStatus(item: ContainerInfo): string {
  if (!item.lastTerminationReason && item.lastExitCode == null && !item.lastFinishedAt) return ''
  const reason = item.lastTerminationReason || 'terminated'
  return `${reason} — exit code: ${item.lastExitCode ?? 0}`
}

// Kubernetes leaves the protocol empty when a manifest does not set one, and
// that default is TCP. Only TCP can be forwarded.
function isTcp(port: ContainerPort): boolean {
  return (port.protocol || 'TCP').toUpperCase() === 'TCP'
}

function portLabel(port: ContainerPort): string {
  const name = port.name ? `${port.name} ` : ''
  return `${name}${port.port}/${(port.protocol || 'TCP').toUpperCase()}`
}

function probeLine(probe: ContainerProbe): string {
  const parts = [probe.kind]
  if (probe.target) parts.push(probe.target)
  if (probe.initialDelay) parts.push(`delay=${probe.initialDelay}s`)
  if (probe.timeout) parts.push(`timeout=${probe.timeout}s`)
  if (probe.period) parts.push(`period=${probe.period}s`)
  if (probe.successThreshold) parts.push(`#success=${probe.successThreshold}`)
  if (probe.failureThreshold) parts.push(`#failure=${probe.failureThreshold}`)
  return parts.join(' ')
}

function envLine(item: EnvVar): string {
  return item.from ? `${item.name} ← ${item.from}` : `${item.name}=${item.value ?? ''}`
}

function envCount(item: ContainerInfo): number {
  return (item.env?.length ?? 0) + (item.envFrom?.length ?? 0)
}

function mountLine(mount: VolumeMount): string {
  const sub = mount.subPath ? `:${mount.subPath}` : ''
  const mode = mount.readOnly ? ' (ro)' : ''
  return `${mount.path} ← ${mount.name}${sub}${mode}`
}

function quantityLine(values: { [_ in string]?: string } | null | undefined): string {
  const entries = Object.entries(values ?? {}).filter((entry): entry is [string, string] => entry[1] != null)
  if (!entries.length) return '—'
  const rank = (key: string) => (key === 'cpu' ? 0 : key === 'memory' ? 1 : 2)
  entries.sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
  return entries
    .map(([key, value]) => {
      const label = key === 'cpu' ? 'CPU' : key === 'memory' ? 'Memory' : key
      return `${label}: ${value}`
    })
    .join(', ')
}

function joined(values: string[] | null | undefined): string {
  return values?.length ? values.join(' ') : '—'
}

const active = computed(() => {
  const byPort = new Map<number, PortForwardSession>()
  for (const session of forwards.forwards) {
    if (session.clusterId !== props.clusterId) continue
    if (session.namespace !== props.namespace) continue
    if (session.resourceType !== 'pod' || session.resourceName !== props.name) continue
    if (
      session.state !== PortForwardState.PortForwardRunning &&
      session.state !== PortForwardState.PortForwardStarting
    ) {
      continue
    }
    byPort.set(session.remotePort, session)
  }
  return byPort
})

function sessionFor(port: number): PortForwardSession | undefined {
  return active.value.get(port)
}

async function forward(port: ContainerPort) {
  if (busy.value[port.port]) return
  busy.value = { ...busy.value, [port.port]: true }
  try {
    const session = await forwards.start(
      props.clusterId,
      props.namespace,
      'pod',
      props.name,
      port.port,
      // Zero asks the backend to pick a free local port.
      0,
    )
    ui.say(`Forwarding localhost:${session.localPort} to ${props.name}:${session.remotePort}.`)
  } catch (err) {
    ui.say(message(err), 'bad')
  } finally {
    busy.value = { ...busy.value, [port.port]: false }
  }
}

async function stop(id: string) {
  if (!id) return
  try {
    await forwards.stop(id)
  } catch (err) {
    ui.say(message(err), 'bad')
  }
}
</script>

<template>
  <div>
    <p v-if="error" class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">{{ error }}</p>
    <p v-else-if="!detail" class="text-sm text-ink-faint">Loading…</p>

    <template v-else>
      <section>
        <h2 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">Properties</h2>
        <dl class="mt-3 space-y-2.5 text-xs">
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Created</dt>
            <dd class="text-ink">{{ agoClock(detail.createdAt) }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Name</dt>
            <dd class="truncate font-mono text-ink">{{ name }}</dd>
          </div>
          <div v-if="namespace" class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Namespace</dt>
            <dd class="truncate font-mono text-ink">{{ namespace }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Labels</dt>
            <dd>
              <button class="text-brand hover:underline" @click="toggle('labels')">
                {{ count(labelPairs.length, 'Label') }}
              </button>
              <ul v-if="shown('labels')" class="mt-1.5 space-y-1">
                <li v-for="[key, value] in labelPairs" :key="key" class="break-all font-mono text-ink-muted">
                  {{ key }}={{ value }}
                </li>
              </ul>
            </dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Annotations</dt>
            <dd>
              <button class="text-brand hover:underline" @click="toggle('annotations')">
                {{ count(annotationPairs.length, 'Annotation') }}
              </button>
              <ul v-if="shown('annotations')" class="mt-1.5 space-y-1">
                <li
                  v-for="[key, value] in annotationPairs"
                  :key="key"
                  class="break-all font-mono text-ink-muted"
                >
                  {{ key }}={{ value }}
                </li>
              </ul>
            </dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Controlled By</dt>
            <dd class="text-ink">{{ detail.controlledBy || '—' }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Status</dt>
            <dd class="flex items-center gap-1.5 text-ink">
              <StateDot :health="detail.health" />
              {{ detail.status || '—' }}
            </dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Node</dt>
            <dd class="truncate font-mono text-ink">{{ detail.node || '—' }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Pod IP</dt>
            <dd class="font-mono text-ink">{{ detail.podIp || '—' }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Pod IPs</dt>
            <dd class="font-mono text-ink">{{ detail.podIps?.join(', ') || '—' }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Service Account</dt>
            <dd class="font-mono text-ink">{{ detail.serviceAccount || '—' }}</dd>
          </div>
          <div class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">QoS Class</dt>
            <dd class="text-ink">{{ detail.qosClass || '—' }}</dd>
          </div>
          <div v-if="detail.conditions?.length" class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Conditions</dt>
            <dd class="flex flex-wrap gap-1.5">
              <span
                v-for="condition in detail.conditions"
                :key="condition.type"
                class="rounded-md border px-2 py-0.5"
                :class="
                  condition.status === 'True' ? 'border-ok/40 text-ok' : 'border-line text-ink-muted'
                "
                :title="condition.message || condition.reason || condition.status"
              >
                {{ condition.type }}
              </span>
            </dd>
          </div>
          <div v-if="detail.tolerations?.length" class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Tolerations</dt>
            <dd>
              <button class="text-brand hover:underline" @click="toggle('tolerations')">
                {{ count(detail.tolerations.length, 'Toleration') }}
              </button>
              <ul v-if="shown('tolerations')" class="mt-1.5 space-y-1">
                <li v-for="line in detail.tolerations" :key="line" class="break-all font-mono text-ink-muted">
                  {{ line }}
                </li>
              </ul>
            </dd>
          </div>
          <div v-if="volumeGroups.length" class="grid grid-cols-[8.5rem_1fr] gap-2">
            <dt class="text-ink-faint">Pod Volumes</dt>
            <dd class="space-y-1">
              <div v-for="group in volumeGroups" :key="group.type">
                <button class="text-brand hover:underline" @click="toggle(`volume:${group.type}`)">
                  {{ group.type }} · {{ count(group.names.length, 'volume') }}
                </button>
                <ul v-if="shown(`volume:${group.type}`)" class="mt-1 space-y-1">
                  <li v-for="volume in group.names" :key="volume" class="font-mono text-ink-muted">
                    {{ volume }}
                  </li>
                </ul>
              </div>
            </dd>
          </div>
        </dl>
      </section>

      <section v-for="section in sections" :key="section.title" class="mt-6">
        <h2 class="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">
          {{ section.title }}
        </h2>
        <article
          v-for="item in section.items"
          :key="item.name"
          class="mt-2 rounded-xl border border-line bg-surface-2 px-4 py-3"
        >
          <h3 class="text-sm font-semibold text-ink">{{ item.name }}</h3>
          <dl class="mt-3 space-y-2.5 text-xs">
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Status</dt>
              <dd class="text-ink">
                <span>{{ statusLine(item) }}</span>
                <span class="text-ink-muted"> · {{ item.restartCount }} restarts</span>
                <p v-if="clock(item.startedAt)" class="mt-0.5 text-ink-muted">
                  Started at {{ clock(item.startedAt) }}
                </p>
              </dd>
            </div>
            <div v-if="lastStatus(item)" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Last Status</dt>
              <dd class="text-ink">
                {{ lastStatus(item) }}
                <p v-if="clock(item.lastStartedAt)" class="mt-0.5 text-ink-muted">
                  Started at {{ clock(item.lastStartedAt) }}
                </p>
                <p v-if="clock(item.lastFinishedAt)" class="text-ink-muted">
                  Finished at {{ clock(item.lastFinishedAt) }}
                </p>
              </dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Image</dt>
              <dd class="break-all font-mono text-ink">{{ item.image || '—' }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Ports</dt>
              <dd v-if="!item.ports?.length" class="text-ink-muted">—</dd>
              <dd v-else class="space-y-1.5">
                <div v-for="(port, index) in item.ports" :key="`${port.name ?? ''}-${port.port}-${index}`" class="flex flex-wrap items-center gap-2">
                  <span class="font-mono text-ink">{{ portLabel(port) }}</span>
                  <span v-if="!isTcp(port)" class="text-ink-faint">TCP only</span>
                  <span v-else-if="sessionFor(port.port)" class="inline-flex items-center gap-2">
                    <span class="font-mono text-ink-muted">localhost:{{ sessionFor(port.port)?.localPort }}</span>
                    <button
                      v-if="sessionFor(port.port)?.state === PortForwardState.PortForwardRunning"
                      class="rounded-lg border border-line px-2.5 py-1 text-ink-muted hover:text-ink"
                      @click="openInBrowser(`http://127.0.0.1:${sessionFor(port.port)?.localPort}`)"
                    >
                      Open
                    </button>
                    <button
                      class="rounded-lg border border-line px-2.5 py-1 text-ink-muted hover:text-ink"
                      @click="stop(sessionFor(port.port)?.id ?? '')"
                    >
                      Stop
                    </button>
                  </span>
                  <button
                    v-else
                    class="rounded-lg border border-line px-2.5 py-1 text-ink-muted hover:text-ink disabled:opacity-40"
                    :disabled="busy[port.port]"
                    @click="forward(port)"
                  >
                    Forward
                  </button>
                </div>
              </dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Environment</dt>
              <dd>
                <span v-if="!envCount(item)" class="text-ink-muted">—</span>
                <template v-else>
                  <button class="text-brand hover:underline" @click="toggle(`env:${section.title}:${item.name}`)">
                    {{ count(envCount(item), 'variable') }}
                  </button>
                  <ul v-if="shown(`env:${section.title}:${item.name}`)" class="mt-1.5 space-y-1">
                    <li
                      v-for="(variable, index) in item.env ?? []"
                      :key="`${variable.name}-${index}`"
                      class="break-all font-mono text-ink-muted"
                    >
                      {{ envLine(variable) }}
                    </li>
                    <li v-for="source in item.envFrom ?? []" :key="source" class="break-all font-mono text-ink-muted">
                      {{ source }}
                    </li>
                  </ul>
                </template>
              </dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Mounts</dt>
              <dd>
                <span v-if="!item.mounts?.length" class="text-ink-muted">—</span>
                <template v-else>
                  <button class="text-brand hover:underline" @click="toggle(`mounts:${section.title}:${item.name}`)">
                    {{ count(item.mounts.length, 'mount') }}
                  </button>
                  <ul v-if="shown(`mounts:${section.title}:${item.name}`)" class="mt-1.5 space-y-1">
                    <li v-for="mount in item.mounts" :key="mount.path" class="break-all font-mono text-ink-muted">
                      {{ mountLine(mount) }}
                    </li>
                  </ul>
                </template>
              </dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Command</dt>
              <dd class="break-all font-mono text-ink">{{ joined(item.command) }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Args</dt>
              <dd class="break-all font-mono text-ink">{{ joined(item.args) }}</dd>
            </div>
            <div v-if="item.liveness" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Liveness</dt>
              <dd class="break-all font-mono text-ink">{{ probeLine(item.liveness) }}</dd>
            </div>
            <div v-if="item.readiness" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Readiness</dt>
              <dd class="break-all font-mono text-ink">{{ probeLine(item.readiness) }}</dd>
            </div>
            <div v-if="item.startup" class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Startup</dt>
              <dd class="break-all font-mono text-ink">{{ probeLine(item.startup) }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Requests</dt>
              <dd class="font-mono text-ink">{{ quantityLine(item.requests) }}</dd>
            </div>
            <div class="grid grid-cols-[8.5rem_1fr] gap-2">
              <dt class="text-ink-faint">Limits</dt>
              <dd class="font-mono text-ink">{{ quantityLine(item.limits) }}</dd>
            </div>
          </dl>
        </article>
      </section>
    </template>
  </div>
</template>
