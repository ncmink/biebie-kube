/** Command on macOS, Control elsewhere — matches existing palette binding. */
export function modKey(event: KeyboardEvent): boolean {
  return event.metaKey || event.ctrlKey
}

export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (target.isContentEditable) return true
  if (target.closest('.monaco-editor')) return true
  if (target.closest('.xterm')) return true
  return false
}

export function inMonaco(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && Boolean(target.closest('.monaco-editor'))
}

type Chord = {
  key: string
  shift?: boolean
  alt?: boolean
  /** When false, modifier must not be held (bare key). */
  mod?: boolean
}

export function matchChord(event: KeyboardEvent, chord: Chord): boolean {
  const wantsMod = chord.mod !== false
  if (wantsMod && !modKey(event)) return false
  if (!wantsMod && modKey(event)) return false
  if (Boolean(chord.shift) !== event.shiftKey) return false
  if (Boolean(chord.alt) !== event.altKey) return false
  return event.key === chord.key || event.key.toLowerCase() === chord.key.toLowerCase()
}
