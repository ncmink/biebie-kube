import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import { watch } from 'vue'

import { useSettingsStore } from '@/stores/settings'

const themeId = 'biebie-custom'

let watching = false

function hex(token: string): string {
  return token.startsWith('#') ? token : `#${token}`
}

export function ensureMonacoTheme() {
  const settings = useSettingsStore()

  const define = () => {
    const tokens = settings.editorTokens
    monaco.editor.defineTheme(themeId, {
      base: settings.resolvedMode === 'light' ? 'vs' : 'vs-dark',
      inherit: true,
      rules: [
        { token: 'comment', foreground: tokens.commentText },
        { token: 'number', foreground: tokens.numberText },
        { token: 'string', foreground: tokens.singleQuotedText },
        { token: 'keyword', foreground: tokens.keywordText },
        { token: 'type', foreground: tokens.identifierText },
        { token: 'tag', foreground: tokens.identifierText },
        { token: 'identifier', foreground: tokens.identifierText },
        { token: 'delimiter', foreground: tokens.editorForeground },
      ],
      colors: {
        'editor.background': hex(tokens.editorBackground),
        'editor.foreground': hex(tokens.editorForeground),
        'editorLineNumber.foreground': settings.palette.inkFaint,
        'editorCursor.foreground': settings.palette.brand,
        'editor.selectionBackground': hex(tokens.selectionBackground),
        'editor.lineHighlightBackground': hex(tokens.currentLineBackground),
      },
    })
    monaco.editor.setTheme(themeId)
  }

  define()

  if (!watching) {
    watching = true
    watch(
      () => [settings.editorTokens, settings.resolvedMode, settings.snapshot.editor, settings.snapshot.editorTypography],
      define,
      { deep: true },
    )
  }

  return themeId
}

export function monacoEditorOptions(): monaco.editor.IStandaloneEditorConstructionOptions {
  const settings = useSettingsStore()
  const editor = settings.snapshot.editor
  return {
    theme: themeId,
    fontFamily: settings.editorFontFamily(),
    fontSize: settings.snapshot.editorTypography.fontSize,
    lineHeight: Math.round(settings.snapshot.editorTypography.fontSize * settings.snapshot.editorTypography.lineHeight),
    tabSize: editor.tabSize,
    wordWrap: editor.wordWrap ? 'on' : 'off',
    minimap: { enabled: editor.minimap },
    lineNumbers: editor.lineNumbers ? 'on' : 'off',
    scrollBeyondLastLine: false,
  }
}

export function applyMonacoOptions(instance: monaco.editor.IStandaloneCodeEditor | monaco.editor.IStandaloneDiffEditor) {
  const options = monacoEditorOptions()
  if ('getModifiedEditor' in instance) {
    instance.updateOptions(options)
    return
  }
  instance.updateOptions(options)
}
