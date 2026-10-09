import { onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { inMonaco, isTypingTarget, matchChord, modKey } from '@/composables/shortcutKeys'
import { useClusterStore } from '@/stores/clusters'
import { useUIStore } from '@/stores/ui'

async function closeActiveClusterTab(
  clusters: ReturnType<typeof useClusterStore>,
  router: ReturnType<typeof useRouter>,
  clusterId: string,
) {
  clusters.close(clusterId)
  const next = clusters.activeId
  await router.push(next ? { name: 'overview', params: { clusterId: next } } : { name: 'clusters' })
}

function focusClusterTab(
  clusters: ReturnType<typeof useClusterStore>,
  router: ReturnType<typeof useRouter>,
  index: number,
) {
  const open = clusters.openClusters
  if (!open.length) return
  const cluster = open[Math.min(index, open.length - 1)]
  if (!cluster) return
  clusters.activeId = cluster.id
  void router.push({ name: 'overview', params: { clusterId: cluster.id } })
}

export function useGlobalShortcuts() {
  const ui = useUIStore()
  const clusters = useClusterStore()
  const router = useRouter()
  const route = useRoute()

  function overlayOpen(): boolean {
    return ui.paletteOpen || ui.switcher !== null || ui.shortcutsOpen
  }

  function clusterRouteId(): string {
    return String(route.params.clusterId ?? '')
  }

  function connected(clusterId: string): boolean {
    return clusters.sessions[clusterId]?.state === 'connected'
  }

  function onKeydown(event: KeyboardEvent) {
    if (matchChord(event, { key: 'k', mod: true }) && !event.shiftKey && !event.altKey) {
      if (inMonaco(event.target)) return
      event.preventDefault()
      ui.togglePalette()
      return
    }

    if (matchChord(event, { key: 'Escape', mod: false })) {
      if (ui.shortcutsOpen) {
        event.preventDefault()
        ui.shortcutsOpen = false
        return
      }
      if (ui.switcher) {
        event.preventDefault()
        ui.switcher = null
      }
      return
    }

    if (!modKey(event)) return

    if (matchChord(event, { key: '/', shift: false, alt: false })) {
      event.preventDefault()
      ui.toggleShortcutsSheet()
      return
    }

    if (matchChord(event, { key: ',', shift: false, alt: false })) {
      event.preventDefault()
      void router.push({ name: 'settings' })
      return
    }

    if (matchChord(event, { key: 'k', shift: true, alt: false })) {
      event.preventDefault()
      ui.openSwitcher('cluster')
      return
    }

    if (matchChord(event, { key: 'n', shift: true, alt: false })) {
      const id = clusterRouteId()
      if (!id || !connected(id)) return
      event.preventDefault()
      ui.openSwitcher('namespace')
      return
    }

    if (matchChord(event, { key: 'p', shift: false, alt: false })) {
      const id = clusterRouteId()
      if (!id || !connected(id)) return
      event.preventDefault()
      ui.openSwitcher('kind')
      return
    }

    if (matchChord(event, { key: 'h', shift: true, alt: false })) {
      event.preventDefault()
      void router.push({ name: 'clusters' })
      return
    }

    if (matchChord(event, { key: 'f', shift: true, alt: false })) {
      event.preventDefault()
      void router.push({ name: 'forwards' })
      return
    }

    if (matchChord(event, { key: '0', shift: false, alt: false })) {
      const id = clusterRouteId()
      if (!id) return
      event.preventDefault()
      void router.push({ name: 'overview', params: { clusterId: id } })
      return
    }

    if (overlayOpen()) return

    if (matchChord(event, { key: ']', shift: true, alt: false })) {
      const open = clusters.openClusters
      const id = clusterRouteId()
      if (open.length < 2 || !id) return
      const index = open.findIndex((c) => c.id === id)
      if (index < 0) return
      event.preventDefault()
      focusClusterTab(clusters, router, (index + 1) % open.length)
      return
    }

    if (matchChord(event, { key: '[', shift: true, alt: false })) {
      const open = clusters.openClusters
      const id = clusterRouteId()
      if (open.length < 2 || !id) return
      const index = open.findIndex((c) => c.id === id)
      if (index < 0) return
      event.preventDefault()
      focusClusterTab(clusters, router, (index - 1 + open.length) % open.length)
      return
    }

    if (matchChord(event, { key: 'w', shift: false, alt: true })) {
      const id = clusterRouteId()
      if (!id || !clusters.openIds.includes(id)) return
      event.preventDefault()
      void closeActiveClusterTab(clusters, router, id)
      return
    }

    if (matchChord(event, { key: 'w', shift: false, alt: false })) {
      const id = clusterRouteId()
      if (!id || !clusters.openIds.includes(id)) return
      event.preventDefault()
      void closeActiveClusterTab(clusters, router, id)
      return
    }

    if (!event.shiftKey && !event.altKey && /^[1-9]$/.test(event.key)) {
      const open = clusters.openClusters
      if (!open.length) return
      event.preventDefault()
      const digit = Number(event.key)
      if (digit === 9) focusClusterTab(clusters, router, open.length - 1)
      else focusClusterTab(clusters, router, digit - 1)
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown, true))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown, true))
}
