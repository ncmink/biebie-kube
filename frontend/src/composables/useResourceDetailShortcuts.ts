import { type Ref, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { isTypingTarget, matchChord, modKey } from '@/composables/shortcutKeys'
import { useUIStore } from '@/stores/ui'

export function useResourceDetailShortcuts(options: {
  clusterId: string
  kind: string
  namespace: string
  name: string
  tabs: Ref<string[]>
  tab: Ref<string>
}) {
  const route = useRoute()
  const router = useRouter()
  const ui = useUIStore()

  const active = () =>
    route.name === 'resource' &&
    String(route.params.clusterId) === options.clusterId &&
    String(route.params.kind) === options.kind &&
    String(route.params.namespace) === options.namespace &&
    String(route.params.name) === options.name

  function onKeydown(event: KeyboardEvent) {
    if (!active()) return
    if (ui.paletteOpen || ui.switcher || ui.shortcutsOpen) return

    if (modKey(event) && event.altKey && !event.shiftKey && /^[1-6]$/.test(event.key)) {
      const index = Number(event.key) - 1
      const name = options.tabs.value[index]
      if (!name) return
      event.preventDefault()
      options.tab.value = name
      return
    }

    if (!modKey(event) && event.key === 'Escape' && !isTypingTarget(event.target)) {
      event.preventDefault()
      void router.push({
        name: 'resources',
        params: { clusterId: options.clusterId, kind: options.kind },
      })
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown, true))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown, true))
}
