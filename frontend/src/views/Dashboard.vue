<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import AddClusterDialog from '@/components/cluster/AddClusterDialog.vue'
import ClusterCard from '@/components/cluster/ClusterCard.vue'
import { useClusterStore } from '@/stores/clusters'

const clusters = useClusterStore()
const router = useRouter()
const adding = ref(false)

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
        <button
          class="rounded-lg bg-brand px-3 py-2 text-sm font-semibold text-surface-1 hover:bg-brand-strong"
          @click="adding = true"
        >
          Add cluster
        </button>
      </div>

      <p v-if="clusters.error" class="mt-6 rounded-xl border border-bad/40 bg-bad/10 px-4 py-3 text-sm">
        {{ clusters.error }}
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

      <section v-for="group in clusters.byCustomer" :key="group.customer" class="mt-8">
        <h2 class="text-xs font-semibold uppercase tracking-widest text-ink-faint">
          {{ group.customer }}
        </h2>
        <div class="mt-3 grid gap-3 md:grid-cols-2">
          <ClusterCard
            v-for="cluster in group.items"
            :key="cluster.id"
            :cluster="cluster"
            @open="open(cluster.id)"
          />
        </div>
      </section>
    </div>

    <AddClusterDialog :open="adding" @close="adding = false" @added="clusters.load()" />
  </main>
</template>
