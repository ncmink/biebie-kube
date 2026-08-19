<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'

import StateDot from '@/components/common/StateDot.vue'
import { api, message } from '@/api'
import { age } from '@/composables/format'
import { Health } from '@/types'
import type { PodDetail } from '@/types'

const props = defineProps<{ clusterId: string; namespace: string; name: string }>()

const detail = ref<PodDetail | null>(null)
const error = ref('')

async function load() {
  error.value = ''
  try {
    detail.value = await api.podDetail(props.clusterId, props.namespace, props.name)
  } catch (err) {
    error.value = message(err)
  }
}

onMounted(load)
watch(() => [props.namespace, props.name], load)
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
