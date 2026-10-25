/**
 * Minimal Markdown → render blocks with HTML escaped everywhere.
 *
 * Ask AI answers are untrusted; nothing from the model is passed through v-html.
 */
export type MarkdownBlock =
  | { kind: 'paragraph'; text: string }
  | { kind: 'code'; text: string }
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'list'; items: string[] }

export function escapeHtml(raw: string): string {
  return raw
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

/** stripInlineMd removes common inline markers for plain display. */
export function stripInlineMd(line: string): string {
  let s = line
  s = s.replace(/\*\*(.+?)\*\*/g, '$1')
  s = s.replace(/`([^`]+)`/g, '$1')
  s = s.replace(/^\s*[-*]\s+/, '')
  return s
}

export function parseSafeMarkdown(source: string): MarkdownBlock[] {
  const lines = source.replace(/\r\n/g, '\n').split('\n')
  const blocks: MarkdownBlock[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    if (line.startsWith('```')) {
      i++
      const code: string[] = []
      while (i < lines.length && !lines[i].startsWith('```')) {
        code.push(lines[i])
        i++
      }
      if (i < lines.length) i++
      blocks.push({ kind: 'code', text: code.join('\n') })
      continue
    }
    if (/^#{1,3}\s+/.test(line)) {
      const level = line.match(/^#+/)?.[0].length ?? 1
      blocks.push({ kind: 'heading', level: Math.min(level, 3), text: stripInlineMd(line.replace(/^#+\s+/, '')) })
      i++
      continue
    }
    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = []
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(stripInlineMd(lines[i]))
        i++
      }
      blocks.push({ kind: 'list', items })
      continue
    }
    if (line.trim() === '') {
      i++
      continue
    }
    const para: string[] = []
    while (i < lines.length && lines[i].trim() !== '' && !lines[i].startsWith('```') && !/^#{1,3}\s+/.test(lines[i])) {
      para.push(stripInlineMd(lines[i]))
      i++
    }
    blocks.push({ kind: 'paragraph', text: para.join(' ') })
  }
  return blocks
}
