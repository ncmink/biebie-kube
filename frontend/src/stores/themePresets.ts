export type ThemePresetId =
  | 'auto'
  | 'light'
  | 'dark'
  | 'silver-dark'
  | 'jade-dark'
  | 'polaris-dark'
  | 'sunset-dark'
  | 'sunset-light'

export interface EditorColorTokens {
  commentText: string
  numberText: string
  singleQuotedText: string
  doubleQuotedText: string
  keywordText: string
  identifierText: string
  functionText: string
  editorBackground: string
  editorForeground: string
  currentLineBackground: string
  selectionBackground: string
}

export interface AppPalette {
  surface0: string
  surface1: string
  surface2: string
  surface3: string
  menu: string
  menuButton: string
  line: string
  lineStrong: string
  ink: string
  inkMuted: string
  inkFaint: string
  brand: string
  brandStrong: string
  ok: string
  warn: string
  bad: string
  info: string
}

export interface TablePalette {
  rowHover: string
  rowSelected: string
  rowStripe: string
  border: string
  text: string
  textMuted: string
}

export interface ThemePreset {
  id: ThemePresetId
  label: string
  group: 'system' | 'builtin'
  description: string
  /** When set, appearance follows the OS instead of a fixed mode. */
  followsSystem?: boolean
  mode: 'light' | 'dark'
  palette: AppPalette
  table: TablePalette
  editor: EditorColorTokens
  /** Mini swatch colours for the theme picker. */
  swatch: [string, string, string]
}

const darkEditorDefaults: EditorColorTokens = {
  commentText: '009B31',
  numberText: 'B57AFF',
  singleQuotedText: 'CA6E6E',
  doubleQuotedText: 'E04978',
  keywordText: '529BD8',
  identifierText: '529BD8',
  functionText: '7CB342',
  editorBackground: '111114',
  editorForeground: 'f4f4f5',
  currentLineBackground: '292929',
  selectionBackground: '134C83',
}

const biebieDarkPalette: AppPalette = {
  surface0: '#1c1d1f',
  surface1: '#111114',
  surface2: '#17171c',
  surface3: '#1f1f26',
  menu: '#262628',
  menuButton: '#222325',
  line: '#2a2a33',
  lineStrong: '#3a3a46',
  ink: '#f4f4f5',
  inkMuted: '#a1a1aa',
  inkFaint: '#71717a',
  brand: '#cbb6e8',
  brandStrong: '#b89ad9',
  ok: '#22c55e',
  warn: '#f59e0b',
  bad: '#ef4444',
  info: '#38bdf8',
}

const biebieLightPalette: AppPalette = {
  surface0: '#f7f7f8',
  surface1: '#ffffff',
  surface2: '#ffffff',
  surface3: '#f1f1f4',
  menu: '#ffffff',
  menuButton: '#f1f1f4',
  line: '#e4e4e8',
  lineStrong: '#cfcfd6',
  ink: '#1c1d1f',
  inkMuted: '#52525b',
  inkFaint: '#8a8a94',
  brand: '#7c5cad',
  brandStrong: '#6b4b9c',
  ok: '#15803d',
  warn: '#b45309',
  bad: '#b91c1c',
  info: '#0369a1',
}

function darkTable(p: AppPalette): TablePalette {
  return {
    rowHover: p.surface3,
    rowSelected: `${p.brand}1a`,
    rowStripe: p.surface2,
    border: p.line,
    text: p.ink,
    textMuted: p.inkMuted,
  }
}

function lightTable(p: AppPalette): TablePalette {
  return darkTable(p)
}

const lightEditor: EditorColorTokens = {
  commentText: '008000',
  numberText: '795E26',
  singleQuotedText: 'A31515',
  doubleQuotedText: 'A31515',
  keywordText: '0000FF',
  identifierText: '001080',
  functionText: '795E26',
  editorBackground: 'ffffff',
  editorForeground: '1c1d1f',
  currentLineBackground: 'f1f1f4',
  selectionBackground: 'add6ff',
}

export const themePresets: ThemePreset[] = [
  {
    id: 'auto',
    label: 'Auto',
    group: 'system',
    description: 'System theme',
    followsSystem: true,
    mode: 'dark',
    palette: biebieDarkPalette,
    table: darkTable(biebieDarkPalette),
    editor: darkEditorDefaults,
    swatch: ['#1c1d1f', '#f4f4f5', '#cbb6e8'],
  },
  {
    id: 'light',
    label: 'Light',
    group: 'system',
    description: 'System theme — default',
    mode: 'light',
    palette: biebieLightPalette,
    table: lightTable(biebieLightPalette),
    editor: lightEditor,
    swatch: ['#ffffff', '#1c1d1f', '#7c5cad'],
  },
  {
    id: 'dark',
    label: 'Dark',
    group: 'system',
    description: 'System theme — default',
    mode: 'dark',
    palette: biebieDarkPalette,
    table: darkTable(biebieDarkPalette),
    editor: darkEditorDefaults,
    swatch: ['#111114', '#f4f4f5', '#cbb6e8'],
  },
  {
    id: 'silver-dark',
    label: 'Silver Dark',
    group: 'builtin',
    description: 'Dark theme',
    mode: 'dark',
    palette: {
      ...biebieDarkPalette,
      surface0: '#1a1b1e',
      surface1: '#121316',
      surface2: '#18191d',
      brand: '#c0c4ce',
      brandStrong: '#a8adb8',
    },
    table: darkTable({
      ...biebieDarkPalette,
      surface0: '#1a1b1e',
      surface3: '#222328',
    }),
    editor: { ...darkEditorDefaults, selectionBackground: '3d4450' },
    swatch: ['#121316', '#d4d4d8', '#c0c4ce'],
  },
  {
    id: 'jade-dark',
    label: 'Jade Dark',
    group: 'builtin',
    description: 'Dark theme',
    mode: 'dark',
    palette: {
      ...biebieDarkPalette,
      surface0: '#0f1412',
      surface1: '#0a0e0c',
      surface2: '#101613',
      brand: '#6ee7b7',
      brandStrong: '#34d399',
    },
    table: darkTable({ ...biebieDarkPalette, surface0: '#0f1412', surface3: '#162019' }),
    editor: { ...darkEditorDefaults, commentText: '34d399', keywordText: '6ee7b7' },
    swatch: ['#0a0e0c', '#d1fae5', '#34d399'],
  },
  {
    id: 'polaris-dark',
    label: 'Polaris Dark',
    group: 'builtin',
    description: 'Dark theme',
    mode: 'dark',
    palette: {
      ...biebieDarkPalette,
      surface0: '#0e1318',
      surface1: '#0a0f14',
      surface2: '#111820',
      brand: '#88c0d0',
      brandStrong: '#5e81ac',
    },
    table: darkTable({ ...biebieDarkPalette, surface0: '#0e1318', surface3: '#1a2430' }),
    editor: { ...darkEditorDefaults, keywordText: '88c0d0', selectionBackground: '2e4a66' },
    swatch: ['#0a0f14', '#eceff4', '#88c0d0'],
  },
  {
    id: 'sunset-dark',
    label: 'Sunset Dark',
    group: 'builtin',
    description: 'Dark theme',
    mode: 'dark',
    palette: {
      ...biebieDarkPalette,
      surface0: '#1a1218',
      surface1: '#120c10',
      surface2: '#181018',
      brand: '#f9b746',
      brandStrong: '#e8952e',
    },
    table: darkTable({ ...biebieDarkPalette, surface0: '#1a1218', surface3: '#241820' }),
    editor: { ...darkEditorDefaults, functionText: 'f9b746', doubleQuotedText: 'e04978' },
    swatch: ['#120c10', '#fce7f3', '#f9b746'],
  },
  {
    id: 'sunset-light',
    label: 'Sunset Light',
    group: 'builtin',
    description: 'Light theme',
    mode: 'light',
    palette: {
      ...biebieLightPalette,
      surface0: '#faf6f0',
      surface1: '#fffaf5',
      brand: '#c2410c',
      brandStrong: '#9a3412',
    },
    table: lightTable(biebieLightPalette),
    editor: {
      ...lightEditor,
      keywordText: 'c2410c',
      functionText: 'b45309',
    },
    swatch: ['#fffaf5', '#431407', '#f9b746'],
  },
]

export function presetById(id: ThemePresetId): ThemePreset {
  return themePresets.find((p) => p.id === id) ?? themePresets.find((p) => p.id === 'dark')!
}

export const editorFontPresets = [
  { id: 'system-mono', label: 'System Mono (Default)', value: "ui-monospace, 'SF Mono', 'JetBrains Mono', monospace" },
  { id: 'sf-mono', label: 'SF Mono', value: "'SF Mono', ui-monospace, monospace" },
  { id: 'jetbrains', label: 'JetBrains Mono', value: "'JetBrains Mono', ui-monospace, monospace" },
  { id: 'menlo', label: 'Menlo', value: 'Menlo, ui-monospace, monospace' },
  { id: 'fira', label: 'Fira Code', value: "'Fira Code', ui-monospace, monospace" },
  { id: 'monaco', label: 'Monaco', value: 'Monaco, ui-monospace, monospace' },
  { id: 'cascadia', label: 'Cascadia Mono', value: "'Cascadia Mono', ui-monospace, monospace" },
] as const

export const tableFontPresets = [
  { id: 'system-sans', label: 'System Sans (Default)', value: 'inherit' },
  { id: 'nunito', label: 'Nunito', value: "'Nunito', ui-sans-serif, system-ui, sans-serif" },
  { id: 'inter', label: 'Inter', value: 'Inter, ui-sans-serif, system-ui, sans-serif' },
  { id: 'sf-pro', label: 'SF Pro Text', value: "'SF Pro Text', -apple-system, system-ui, sans-serif" },
  { id: 'system-mono', label: 'System Mono', value: "ui-monospace, 'SF Mono', monospace" },
] as const

export const appFontPresets = [
  { id: 'nunito', label: 'Nunito (Default)', value: "'Nunito', ui-sans-serif, system-ui, sans-serif" },
  { id: 'system', label: 'System UI', value: 'system-ui, -apple-system, sans-serif' },
  { id: 'sf-pro', label: 'SF Pro', value: "'SF Pro Text', -apple-system, sans-serif" },
  { id: 'inter', label: 'Inter', value: 'Inter, ui-sans-serif, sans-serif' },
  { id: 'jetbrains', label: 'JetBrains Mono', value: "'JetBrains Mono', ui-monospace, monospace" },
] as const

export type EditorTokenKey = keyof EditorColorTokens

export const editorTokenLabels: Record<EditorTokenKey, string> = {
  commentText: 'Comment text',
  numberText: 'Number text',
  singleQuotedText: 'Single quoted text',
  doubleQuotedText: 'Double quoted text',
  keywordText: 'Keyword text',
  identifierText: 'Identifier text',
  functionText: 'Function text',
  editorBackground: 'Editor background',
  editorForeground: 'Editor foreground',
  currentLineBackground: 'Current line background',
  selectionBackground: 'Selection background',
}
