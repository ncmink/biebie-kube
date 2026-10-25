import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { allReleaseNotes, type ReleaseNote } from '@/releaseNotes'
import { useSettingsStore, type Appearance } from '@/stores/settings'

export type { Appearance } from '@/stores/settings'
export type SwitcherKind = 'cluster' | 'namespace' | 'kind'

/** Chrome-level UI state: overlays and notices. Appearance lives in settings. */
export const useUIStore = defineStore('ui', () => {
  const paletteOpen = ref(false)
  const switcher = ref<SwitcherKind | null>(null)
  const shortcutsOpen = ref(false)
  const whatsNewOpen = ref(false)
  const whatsNewNotes = ref<ReleaseNote[]>([])
  const whatsNewMarkOnDismiss = ref(false)
  const notice = ref<{ tone: 'ok' | 'bad'; text: string } | null>(null)

  let noticeTimer: number | undefined

  function settings() {
    return useSettingsStore()
  }

  function apply() {
    settings().apply()
  }

  function setAppearance(next: Appearance) {
    settings().setAppearance(next)
  }

  function say(text: string, tone: 'ok' | 'bad' = 'ok') {
    notice.value = { tone, text }
    window.clearTimeout(noticeTimer)
    const duration = tone === 'bad' ? settings().snapshot.general.noticeBadMs : settings().snapshot.general.noticeOkMs
    noticeTimer = window.setTimeout(() => {
      notice.value = null
    }, duration)
  }

  function dismiss() {
    notice.value = null
    window.clearTimeout(noticeTimer)
  }

  function closeOverlays() {
    paletteOpen.value = false
    switcher.value = null
    shortcutsOpen.value = false
    whatsNewOpen.value = false
  }

  function openWhatsNew(notes: ReleaseNote[], markOnDismiss = false) {
    paletteOpen.value = false
    switcher.value = null
    shortcutsOpen.value = false
    whatsNewNotes.value = notes
    whatsNewMarkOnDismiss.value = markOnDismiss
    whatsNewOpen.value = true
  }

  function openWhatsNewCatalog() {
    openWhatsNew(allReleaseNotes(), false)
  }

  function closeWhatsNew() {
    whatsNewOpen.value = false
    whatsNewMarkOnDismiss.value = false
  }

  function openPalette() {
    switcher.value = null
    shortcutsOpen.value = false
    paletteOpen.value = true
  }

  function togglePalette() {
    if (paletteOpen.value) {
      paletteOpen.value = false
      return
    }
    openPalette()
  }

  function openSwitcher(kind: SwitcherKind) {
    paletteOpen.value = false
    shortcutsOpen.value = false
    switcher.value = kind
  }

  function toggleShortcutsSheet() {
    if (shortcutsOpen.value) {
      shortcutsOpen.value = false
      return
    }
    paletteOpen.value = false
    switcher.value = null
    shortcutsOpen.value = true
  }

  const appearance = computed(() => settings().appearance)

  return {
    appearance,
    paletteOpen,
    switcher,
    shortcutsOpen,
    whatsNewOpen,
    whatsNewNotes,
    whatsNewMarkOnDismiss,
    notice,
    setAppearance,
    say,
    dismiss,
    apply,
    closeOverlays,
    openPalette,
    togglePalette,
    openSwitcher,
    toggleShortcutsSheet,
    openWhatsNew,
    openWhatsNewCatalog,
    closeWhatsNew,
  }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useUIStore, import.meta.hot))
}
