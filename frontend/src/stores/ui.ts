import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

export type Appearance = 'dark' | 'light' | 'system'

const storageKey = 'biebie-kube.appearance'

/** Chrome-level UI state: appearance, the command palette, and notices. */
export const useUIStore = defineStore('ui', () => {
  const appearance = ref<Appearance>(readAppearance())
  const paletteOpen = ref(false)
  const notice = ref<{ tone: 'ok' | 'bad'; text: string } | null>(null)

  let noticeTimer: number | undefined

  function readAppearance(): Appearance {
    const stored = localStorage.getItem(storageKey)
    return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'dark'
  }

  /**
   * apply switches the palette by toggling one class on the root element, so
   * every component follows without knowing appearance exists.
   */
  function apply() {
    const system = window.matchMedia('(prefers-color-scheme: light)').matches
    const light = appearance.value === 'light' || (appearance.value === 'system' && system)
    document.documentElement.classList.toggle('theme-light', light)
  }

  function setAppearance(next: Appearance) {
    appearance.value = next
    localStorage.setItem(storageKey, next)
    apply()
  }

  function say(text: string, tone: 'ok' | 'bad' = 'ok') {
    notice.value = { tone, text }
    window.clearTimeout(noticeTimer)
    noticeTimer = window.setTimeout(() => {
      notice.value = null
    }, tone === 'bad' ? 8000 : 4000)
  }

  function dismiss() {
    notice.value = null
    window.clearTimeout(noticeTimer)
  }

  // Following the system means following it as it changes, not only at start.
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (appearance.value === 'system') apply()
  })

  watch(appearance, apply, { immediate: true })

  return { appearance, paletteOpen, notice, setAppearance, say, dismiss, apply }
})
