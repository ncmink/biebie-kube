<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { api, message, openInBrowser } from '@/api'
import { age } from '@/composables/format'
import { usePortForwardStore } from '@/stores/sessions'
import { useUIStore } from '@/stores/ui'
import { Health, PortForwardState } from '@/types'
import type { ContainerPort, PodDetail, PortForwardSession } from '@/types'

const props = defineProps<{ clusterId: string; namespace: string; name: string }>()

// The detail page keeps the ports so the header's port-forward dialog can
// offer them, without fetching the pod a second time.
const emit = defineEmits<{ loaded: [detail: PodDetail] }>()

const forwards = usePortForwardStore()
const ui = useUIStore()

const detail = ref<PodDetail | null>(null)
const error = ref('')
const busy = ref<Record<number, boolean>>({})

async function load() {
  error.value = ''
  try {
    detail.value = await api.podDetail(props.clusterId, props.namespace, props.name)
    if (detail.value) emit('loaded', detail.value)
  } catch (err) {
    error.value = message(err)
  }
}

onMounted(load)
watch(() => [props.namespace, props.name], load)

// Kubernetes leaves the protocol empty when a manifest does not set one, and
// that default is TCP. Only TCP can be forwarded.
function isTcp(port: ContainerPort): boolean {
  return (port.protocol || 'TCP').toUpperCase() === 'TCP'
}

function portLabel(port: ContainerPort): string {
  return `${port.port}/${(port.protocol || 'TCP').toUpperCase()}`
}

function sessionFor(port: number): PortForwardSession | undefined {
  return active.value.get(port)
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
  <div class="h-full overflow-y-auto px-6 py-5">
    <p v-if="error" class="rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">{{ error }}</p>

    <template v-else-if="detail">
      <dl class="grid gap-4 sm:grid-cols-4">
        <div>
          <dt class="text-xs text-ink-faint">Status</dt>
          <dd class="mt-1 flex items-center gap-1.5 text-sm text-ink">
            <StateDot :health="detail.health" />
            {{ detail.status }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ink-faint">Node</dt>
          <dd class="mt-1 truncate text-sm text-ink">{{ detail.node || '—' }}</dd>
        </div>
        <div>
          <dt class="text-xs text-ink-faint">Pod IP</dt>
          <dd class="mt-1 font-mono text-sm text-ink">{{ detail.podIp || '—' }}</dd>
        </div>
        <div>
          <dt class="text-xs text-ink-faint">Age</dt>
          <dd class="mt-1 font-mono text-sm text-ink">{{ age(detail.startedAt) }}</dd>
        </div>
      </dl>

      <section class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">Containers</h2>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <table class="w-full text-sm">
            <tbody class="divide-y divide-line">
              <tr
                v-for="item in [...(detail.initContainers ?? []), ...(detail.containers ?? [])]"
                :key="item.name"
                class="bg-surface-2"
              >
                <td class="px-4 py-2.5">
                  <span class="flex items-center gap-2">
                    <StateDot :health="item.ready ? Health.HealthHealthy : Health.HealthWarning" />
                    <span class="text-ink">{{ item.name }}</span>
                    <span v-if="item.init" class="text-[10px] uppercase tracking-widest text-ink-faint">
                      init
                    </span>
                  </span>
                </td>
                <td class="max-w-80 truncate px-4 py-2.5 font-mono text-xs text-ink-muted">
                  {{ item.image }}
                </td>
                <td class="px-4 py-2.5 text-xs text-ink-muted">{{ item.state || '—' }}</td>
                <td class="px-4 py-2.5 text-right font-mono text-xs text-ink-faint">
                  {{ item.restartCount }} restarts
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="detail.ports?.length" class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">Ports</h2>
        <div class="mt-2 overflow-hidden rounded-xl border border-line">
          <table class="w-full text-sm">
            <tbody class="divide-y divide-line">
              <tr
                v-for="(port, index) in detail.ports"
                :key="`${port.name ?? ''}-${port.port}-${port.protocol}-${index}`"
                class="bg-surface-2"
              >
                <td class="px-4 py-2.5 text-ink">{{ port.name || '—' }}</td>
                <td class="px-4 py-2.5 font-mono text-xs text-ink-muted">{{ portLabel(port) }}</td>
                <td class="px-4 py-2.5 text-right">
                  <span v-if="!isTcp(port)" class="text-xs text-ink-faint">TCP only</span>
                  <span v-else-if="sessionFor(port.port)" class="inline-flex items-center gap-2">
                    <span class="font-mono text-xs text-ink-muted">
                      localhost:{{ sessionFor(port.port)?.localPort }}
                    </span>
                    <button
                      v-if="sessionFor(port.port)?.state === PortForwardState.PortForwardRunning"
                      class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
                      @click="openInBrowser(`http://127.0.0.1:${sessionFor(port.port)?.localPort}`)"
                    >
                      Open
                    </button>
                    <button
                      class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink"
                      @click="stop(sessionFor(port.port)?.id ?? '')"
                    >
                      Stop
                    </button>
                  </span>
                  <button
                    v-else
                    class="rounded-lg border border-line px-2.5 py-1 text-xs text-ink-muted hover:text-ink disabled:opacity-40"
                    :disabled="busy[port.port]"
                    @click="forward(port)"
                  >
                    Forward
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="detail.conditions?.length" class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">Conditions</h2>
        <ul class="mt-2 space-y-1">
          <li
            v-for="condition in detail.conditions"
            :key="condition.type"
            class="flex items-start gap-2 text-xs"
          >
            <StateDot
              :health="condition.status === 'True' ? Health.HealthHealthy : Health.HealthWarning"
              class="mt-1"
            />
            <span class="w-40 shrink-0 text-ink">{{ condition.type }}</span>
            <span class="text-ink-muted">{{ condition.message || condition.reason || condition.status }}</span>
          </li>
        </ul>
      </section>

      <section v-if="detail.labels && Object.keys(detail.labels).length" class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">Labels</h2>
        <div class="mt-2 flex flex-wrap gap-1.5">
          <span
            v-for="(value, key) in detail.labels"
            :key="key"
            class="rounded-md border border-line bg-surface-2 px-2 py-0.5 font-mono text-[11px] text-ink-muted"
          >
            {{ key }}={{ value }}
          </span>
        </div>
      </section>

      <section v-if="detail.volumes?.length" class="mt-6">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">Volumes</h2>
        <p class="mt-2 font-mono text-xs text-ink-muted">{{ detail.volumes.join(', ') }}</p>
      </section>
    </template>
  </div>
</template>
