import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'

import {
  appFontPresets,
  type EditorColorTokens,
  type EditorTokenKey,
  editorFontPresets,
  editorTokenLabels,
  presetById,
  tableFontPresets,
  type ThemePresetId,
} from '@/stores/themePresets'

export type SettingsTab =
  | 'general'
  | 'table'
  | 'editor'
  | 'fonts'
  | 'locations'
  | 'authoring'
  | 'ai'
  | 'keymap'

export type FontsSurface = 'editor' | 'table' | 'application'

export type AgeFormat = 'relative' | 'iso'

const storageKey = 'biebie-kube.settings.v1'
const legacyAppearanceKey = 'biebie-kube.appearance'
const openClustersKey = 'biebie-kube.open-cluster-ids'

export interface TypographySettings {
  fontPresetId: string
  fontSize: number
  lineHeight: number
}

export interface SettingsSnapshot {
  themePreset: ThemePresetId
  tokenOverrides: Partial<EditorColorTokens>
  editorTypography: TypographySettings
  tableTypography: TypographySettings
  appTypography: TypographySettings
  general: {
    reopenClusters: boolean
    autoCheckUpdates: boolean
    noticeOkMs: number
    noticeBadMs: number
  }
  table: {
    striping: boolean
    showNamespaceWhenAll: boolean
    ageFormat: AgeFormat
    density: number
  }
  editor: {
    tabSize: 2 | 4
    wordWrap: boolean
    lineNumbers: boolean
    minimap: boolean
    diffSideBySide: boolean
    logTailLines: number
    logWrap: boolean
    logTimestamps: boolean
    terminalFontSize: number
    cursorBlink: boolean
  }
}

function defaultSnapshot(): SettingsSnapshot {
  return {
    themePreset: 'dark',
    tokenOverrides: {},
    editorTypography: { fontPresetId: 'system-mono', fontSize: 13, lineHeight: 1.4 },
    tableTypography: { fontPresetId: 'system-sans', fontSize: 13, lineHeight: 1.25 },
    appTypography: { fontPresetId: 'nunito', fontSize: 14, lineHeight: 1.35 },
    general: {
      reopenClusters: true,
      autoCheckUpdates: true,
      noticeOkMs: 4000,
      noticeBadMs: 8000,
    },
    table: {
      striping: false,
      showNamespaceWhenAll: true,
      ageFormat: 'relative',
      density: 1.25,
    },
    editor: {
      tabSize: 2,
      wordWrap: false,
      lineNumbers: true,
      minimap: false,
      diffSideBySide: true,
      logTailLines: 500,
      logWrap: false,
      logTimestamps: false,
      terminalFontSize: 12,
      cursorBlink: true,
    },
  }
}

function migrateLegacyAppearance(snapshot: SettingsSnapshot): SettingsSnapshot {
  const legacy = localStorage.getItem(legacyAppearanceKey)
  if (legacy === 'light') snapshot.themePreset = 'light'
  else if (legacy === 'system') snapshot.themePreset = 'auto'
  else if (legacy === 'dark') snapshot.themePreset = 'dark'
  return snapshot
}

function readSnapshot(): SettingsSnapshot {
  try {
    const raw = localStorage.getItem(storageKey)
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<SettingsSnapshot>
      return { ...defaultSnapshot(), ...parsed, general: { ...defaultSnapshot().general, ...parsed.general }, table: { ...defaultSnapshot().table, ...parsed.table }, editor: { ...defaultSnapshot().editor, ...parsed.editor } }
    }
  } catch {
    // fall through
  }
  return migrateLegacyAppearance(defaultSnapshot())
}

function persist(snapshot: SettingsSnapshot) {
  localStorage.setItem(storageKey, JSON.stringify(snapshot))
}

/** Appearance value kept compatible with the older UI store. */
export type Appearance = 'dark' | 'light' | 'system'

export const useSettingsStore = defineStore('settings', () => {
  const snapshot = ref<SettingsSnapshot>(readSnapshot())
  const settingsOpen = ref(false)
  const activeTab = ref<SettingsTab>('general')
  const fontsSurface = ref<FontsSurface>('editor')

  const themePreset = computed(() => snapshot.value.themePreset)
  const activePreset = computed(() => presetById(snapshot.value.themePreset))

  const resolvedMode = computed<'light' | 'dark'>(() => {
    const preset = activePreset.value
    if (preset.followsSystem) {
      return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
    }
    return preset.mode
  })

  const editorTokens = computed<EditorColorTokens>(() => ({
    ...activePreset.value.editor,
    ...snapshot.value.tokenOverrides,
  }))

  const palette = computed(() => effectivePalette())

  const appearance = computed<Appearance>(() => {
    if (snapshot.value.themePreset === 'auto') return 'system'
    if (snapshot.value.themePreset === 'light' || snapshot.value.themePreset === 'sunset-light') return 'light'
    return 'dark'
  })

  function effectivePalette() {
    const preset = activePreset.value
    if (preset.followsSystem) {
      return presetById(resolvedMode.value === 'light' ? 'light' : 'dark').palette
    }
    return preset.palette
  }

  function effectiveTablePalette() {
    const preset = activePreset.value
    if (preset.followsSystem) {
      return presetById(resolvedMode.value === 'light' ? 'light' : 'dark').table
    }
    return preset.table
  }

  function tableRowHeight(): number {
    const size = snapshot.value.tableTypography.fontSize
    const density = snapshot.value.table.density
    return Math.round(size * density + 8)
  }

  function editorFontFamily(): string {
    const preset = editorFontPresets.find((p) => p.id === snapshot.value.editorTypography.fontPresetId)
    return preset?.value ?? editorFontPresets[0].value
  }

  function tableFontFamily(): string {
    const preset = tableFontPresets.find((p) => p.id === snapshot.value.tableTypography.fontPresetId)
    if (!preset || preset.value === 'inherit') return ''
    return preset.value
  }

  function appFontFamily(): string {
    const preset = appFontPresets.find((p) => p.id === snapshot.value.appTypography.fontPresetId)
    return preset?.value ?? appFontPresets[0].value
  }

  function apply() {
    const palette = effectivePalette()
    const table = effectiveTablePalette()
    const root = document.documentElement

    root.classList.toggle('theme-light', resolvedMode.value === 'light')
    root.dataset.themePreset = snapshot.value.themePreset

    const vars: Record<string, string> = {
      '--color-surface-0': palette.surface0,
      '--color-surface-1': palette.surface1,
      '--color-surface-2': palette.surface2,
      '--color-surface-3': palette.surface3,
      '--color-menu': palette.menu,
      '--color-menu-button': palette.menuButton,
      '--color-line': palette.line,
      '--color-line-strong': palette.lineStrong,
      '--color-ink': palette.ink,
      '--color-ink-muted': palette.inkMuted,
      '--color-ink-faint': palette.inkFaint,
      '--color-brand': palette.brand,
      '--color-brand-strong': palette.brandStrong,
      '--color-ok': palette.ok,
      '--color-warn': palette.warn,
      '--color-bad': palette.bad,
      '--color-info': palette.info,
      '--table-row-hover': table.rowHover,
      '--table-row-selected': table.rowSelected,
      '--table-row-stripe': table.rowStripe,
      '--table-border': table.border,
      '--editor-font-family': editorFontFamily(),
      '--editor-font-size': `${snapshot.value.editorTypography.fontSize}px`,
      '--editor-line-height': String(snapshot.value.editorTypography.lineHeight),
      '--table-font-size': `${snapshot.value.tableTypography.fontSize}px`,
      '--table-row-height': `${tableRowHeight()}px`,
      '--app-font-size': `${snapshot.value.appTypography.fontSize}px`,
    }

    const tableFont = tableFontFamily()
    if (tableFont) vars['--table-font-family'] = tableFont

    vars['--font-sans'] = appFontFamily()

    for (const [key, value] of Object.entries(vars)) {
      root.style.setProperty(key, value)
    }

    for (const [key, hex] of Object.entries(editorTokens.value)) {
      root.style.setProperty(`--editor-${key.replace(/[A-Z]/g, (m) => `-${m.toLowerCase()}`)}`, `#${hex.replace('#', '')}`)
    }
  }

  function save() {
    persist(snapshot.value)
  }

  function setThemePreset(id: ThemePresetId) {
    snapshot.value.themePreset = id
    snapshot.value.tokenOverrides = {}
    save()
    apply()
  }

  function setTokenOverride(key: EditorTokenKey, hex: string) {
    const normalized = hex.replace('#', '').toUpperCase()
    snapshot.value.tokenOverrides = { ...snapshot.value.tokenOverrides, [key]: normalized }
    save()
    apply()
  }

  function resetTokenOverrides() {
    snapshot.value.tokenOverrides = {}
    save()
    apply()
  }

  function setAppearance(next: Appearance) {
    if (next === 'system') setThemePreset('auto')
    else if (next === 'light') setThemePreset('light')
    else setThemePreset('dark')
  }

  function openSettings(tab?: SettingsTab) {
    if (tab) activeTab.value = tab
    settingsOpen.value = true
  }

  function closeSettings() {
    settingsOpen.value = false
  }

  function persistOpenClusters(ids: string[]) {
    if (!snapshot.value.general.reopenClusters) return
    localStorage.setItem(openClustersKey, JSON.stringify(ids))
  }

  function readOpenClusters(): string[] {
    if (!snapshot.value.general.reopenClusters) return []
    try {
      const raw = localStorage.getItem(openClustersKey)
      if (!raw) return []
      const parsed = JSON.parse(raw)
      return Array.isArray(parsed) ? parsed.filter((id): id is string => typeof id === 'string') : []
    } catch {
      return []
    }
  }

  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (activePreset.value.followsSystem) apply()
  })

  watch(snapshot, save, { deep: true })
  watch(resolvedMode, apply)

  return {
    snapshot,
    settingsOpen,
    activeTab,
    fontsSurface,
    themePreset,
    activePreset,
    resolvedMode,
    editorTokens,
    palette,
    editorTokenLabels,
    appearance,
    apply,
    save,
    setThemePreset,
    setTokenOverride,
    resetTokenOverrides,
    setAppearance,
    openSettings,
    closeSettings,
    tableRowHeight,
    editorFontFamily,
    tableFontFamily,
    appFontFamily,
    persistOpenClusters,
    readOpenClusters,
  }
})

if (import.meta.hot) {
  import.meta.hot.accept(acceptHMRUpdate(useSettingsStore, import.meta.hot))
}
