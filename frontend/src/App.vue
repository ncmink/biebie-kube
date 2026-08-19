<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { RouterView, useRouter } from 'vue-router'

import CommandPalette from '@/components/common/CommandPalette.vue'
import TitleBar from '@/components/common/TitleBar.vue'
import NoticeBar from '@/components/common/NoticeBar.vue'
import { api, events, on } from '@/api'
import { useClusterStore } from '@/stores/clusters'
import { usePortForwardStore } from '@/stores/sessions'
import { useResourceStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'

const clusters = useClusterStore()
const resources = useResourceStore()
const forwards = usePortForwardStore()
const ui = useUIStore()
const router = useRouter()

function onKeydown(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    ui.paletteOpen = !ui.paletteOpen
  }
}

onMounted(async () => {
  ui.apply()
  clusters.subscribe()
  resources.subscribe()
  forwards.subscribe()

  window.addEventListener('keydown', onKeydown)

  await clusters.load()
  await forwards.load()

  /**
   * A handoff from Biebie Access has already chosen the customer and cluster,
   * so this navigates straight there rather than returning the user to a list
   * and asking them to pick the same thing again.
   */
  on(events.openCluster, async (result) => {
    if (result.unmatched || !result.clusterId) {
      ui.say(
        `Biebie Access opened ${result.context.customerName} / ${result.context.clusterName}, which is not configured here yet. Add it to continue.`,
        'bad',
      )
      await router.push({ name: 'clusters' })
      return
    }
    await clusters.load()
    await clusters.open(result.clusterId)
    await router.push({ name: 'overview', params: { clusterId: result.clusterId } })
  })

  on(events.handoffFailed, (reason) => ui.say(reason, 'bad'))

  // Only now can a deep link that arrived before the window existed be acted
  // on: Go holds it until this call, so a handoff is never consumed into a
  // window that was not yet listening.
  void api.ready()
})

onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="flex h-full flex-col bg-surface-0 text-ink">
    <TitleBar />
    <RouterView class="min-h-0 flex-1" />
    <NoticeBar />
    <CommandPalette />
  </div>
</template>
