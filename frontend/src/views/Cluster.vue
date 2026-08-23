<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { RouterView, useRouter } from 'vue-router'

import ClusterTabs from '@/components/cluster/ClusterTabs.vue'
import ConnectionDiagnosis from '@/components/cluster/ConnectionDiagnosis.vue'
import NamespaceSelector from '@/components/cluster/NamespaceSelector.vue'
import ResourceNav from '@/components/resource/ResourceNav.vue'
import { message } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{ clusterId: string }>()

const clusters = useClusterStore()
const ui = useUIStore()
const router = useRouter()

const cluster = computed(() => clusters.clusters.find((c) => c.id === props.clusterId))
const session = computed(() => clusters.sessions[props.clusterId])
const connected = computed(() => session.value?.state === 'connected')
const opening = computed(() => clusters.accessOpening(cluster.value?.access.profileId ?? ''))

/** The button says what it is waiting on rather than inviting another click. */
const accessButton = computed(() => {
  if (clusters.accessAsked[cluster.value?.access.profileId ?? '']) return 'Opening Biebie Access…'
  if (opening.value) return 'Connecting…'
  return 'Connect with Biebie Access'
})

onMounted(() => clusters.open(props.clusterId))

watch(
  () => props.clusterId,
  (id) => {
    if (id) void clusters.open(id)
  },
)

async function connectAccess() {
  if (!cluster.value) return
  try {
    await clusters.connectWithAccess(cluster.value)
    ui.say('Asked Biebie Access to connect this customer. This cluster will retry on its own.')
  } catch (error) {
    ui.say(message(error), 'bad')
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <ClusterTabs />

    <div class="flex min-h-0 flex-1">
      <aside class="flex w-64 shrink-0 flex-col border-r border-line bg-surface-1">
        <NamespaceSelector :cluster-id="clusterId" :disabled="!connected" />
        <ResourceNav :cluster-id="clusterId" class="min-h-0 flex-1" />
      </aside>

      <main class="min-w-0 flex-1 overflow-hidden bg-surface-0">
        <div v-if="connected" class="h-full">
          <RouterView />
        </div>

        <div v-else class="flex h-full items-center justify-center p-8">
          <div class="w-full max-w-lg">
            <p class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
              {{ cluster?.customerName }} / {{ cluster?.environmentName }}
            </p>
            <h2 class="mt-1 text-lg font-semibold text-ink">{{ cluster?.name }}</h2>

            <div v-if="session?.state === 'connecting'" class="mt-5 flex items-center gap-2 text-sm text-ink-muted">
              <span class="size-2 animate-pulse rounded-full bg-info" aria-hidden="true" />
              Checking access and reaching the API server…
            </div>

            <ConnectionDiagnosis v-else-if="session?.diagnosis" :diagnosis="session.diagnosis" class="mt-5">
              <div class="mt-4 flex flex-wrap gap-2">
                <button
                  v-if="session.diagnosis.accessProfileId"
                  class="flex items-center gap-2 rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors"
                  :class="
                    opening
                      ? 'bg-info/15 text-info'
                      : 'bg-brand text-surface-1 hover:bg-brand-strong'
                  "
                  @click="connectAccess"
                >
                  <span
                    v-if="opening"
                    class="size-2 animate-pulse rounded-full bg-info"
                    aria-hidden="true"
                  />
                  {{ accessButton }}
                </button>
                <button
                  class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
                  @click="clusters.connect(clusterId)"
                >
                  Try again
                </button>
                <button
                  class="rounded-lg border border-line px-3 py-1.5 text-xs text-ink-muted hover:text-ink"
                  @click="router.push({ name: 'clusters' })"
                >
                  Back to clusters
                </button>
              </div>
            </ConnectionDiagnosis>

            <div v-else class="mt-5">
              <button
                class="rounded-lg bg-brand px-3 py-1.5 text-sm font-semibold text-surface-1"
                @click="clusters.connect(clusterId)"
              >
                Connect
              </button>
            </div>
          </div>
        </div>
      </main>
    </div>
  </div>
</template>
