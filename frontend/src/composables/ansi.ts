/**
 * A reader for the colours a container writes into its own logs.
 *
 * Nothing between the container and this window strips escape codes: the Go
 * side hands over the line exactly as Kubernetes served it. Rendered as text
 * that shows up as rubbish like "[32mLOG[39m", so the sequences are parsed
 * here into styled runs instead — which is also the only reason the log pane
 * has any colour in it at all.
 *
 * Only SGR ("select graphic rendition") is interpreted. A cursor move or a
 * screen clear means nothing to a list of lines, so those sequences are
 * dropped rather than shown.
 */

/** Segment is a run of text sharing one appearance. */
export interface Segment {
  text: string
  fg?: string
  bg?: string
  bold?: boolean
  dim?: boolean
  italic?: boolean
  underline?: boolean
  /** Set by highlight(), not by the parser: this run matches the search. */
  match?: boolean
}

/** Style is the parser's running state, before inverse video is resolved. */
interface Style {
  fg?: string
  bg?: string
  bold?: boolean
  dim?: boolean
  italic?: boolean
  underline?: boolean
  inverse?: boolean
}

/**
 * Either an SGR sequence, whose parameters are captured, or any other escape
 * sequence, which is matched only so that it can be thrown away.
 *
 * SGR has to come first: its "m" is also a valid CSI final byte, so the
 * general alternative would otherwise swallow the colours. The rest cover CSI
 * (a cursor move, say), OSC under either terminator, and two-byte escapes.
 */
const sequence =
  /\x1b\[([0-9;]*)m|\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]|\x1b\][\s\S]*?(?:\x07|\x1b\\)|\x1b[\x40-\x5a\x5c-\x5f]/g

const names = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white']

/**
 * named points at a theme token rather than a fixed colour.
 *
 * A container picks its own colours, and the same red has to stay readable
 * when the window is in its light appearance, so the palette is the theme's to
 * decide.
 */
function named(index: number, bright: boolean): string {
  return `var(--color-ansi-${bright ? 'bright-' : ''}${names[index]})`
}

/** cube resolves one of the 256 palette entries xterm defines. */
function cube(value: number): string {
  if (value < 8) return named(value, false)
  if (value < 16) return named(value - 8, true)

  if (value < 232) {
    const offset = value - 16
    const level = (step: number) => (step === 0 ? 0 : 55 + step * 40)
    return `rgb(${level(Math.floor(offset / 36))} ${level(Math.floor(offset / 6) % 6)} ${level(offset % 6)})`
  }

  const grey = 8 + (value - 232) * 10
  return `rgb(${grey} ${grey} ${grey})`
}

/**
 * extended reads the parameters that follow a 38 or 48, and reports how many
 * of them it consumed so the caller can step over them.
 */
function extended(codes: number[], at: number): { colour?: string; used: number } {
  if (codes[at + 1] === 5) return { colour: cube(codes[at + 2] ?? 0), used: 2 }
  if (codes[at + 1] === 2) {
    return {
      colour: `rgb(${codes[at + 2] ?? 0} ${codes[at + 3] ?? 0} ${codes[at + 4] ?? 0})`,
      used: 4,
    }
  }
  return { used: 0 }
}

/** apply folds one SGR sequence's parameters into the running style. */
function apply(style: Style, params: string): Style {
  // A bare "\x1b[m" is the same reset as "\x1b[0m".
  if (params === '') return {}

  const codes = params.split(';').map((part) => Number(part) || 0)
  const next: Style = { ...style }

  for (let i = 0; i < codes.length; i += 1) {
    const code = codes[i]

    if (code === 0) {
      // A reset in the middle of a sequence discards what came before it.
      for (const key of Object.keys(next) as (keyof Style)[]) delete next[key]
    } else if (code === 1) next.bold = true
    else if (code === 2) next.dim = true
    else if (code === 3) next.italic = true
    else if (code === 4) next.underline = true
    else if (code === 7) next.inverse = true
    else if (code === 22) {
      delete next.bold
      delete next.dim
    } else if (code === 23) delete next.italic
    else if (code === 24) delete next.underline
    else if (code === 27) delete next.inverse
    else if (code >= 30 && code <= 37) next.fg = named(code - 30, false)
    else if (code === 38) {
      const { colour, used } = extended(codes, i)
      if (colour) next.fg = colour
      i += used
    } else if (code === 39) delete next.fg
    else if (code >= 40 && code <= 47) next.bg = named(code - 40, false)
    else if (code === 48) {
      const { colour, used } = extended(codes, i)
      if (colour) next.bg = colour
      i += used
    } else if (code === 49) delete next.bg
    else if (code >= 90 && code <= 97) next.fg = named(code - 90, true)
    else if (code >= 100 && code <= 107) next.bg = named(code - 100, true)
  }

  return next
}

/** cut turns the running style and a slice of text into a rendered segment. */
function cut(style: Style, text: string): Segment {
  const segment: Segment = { text }

  // Inverse video swaps the two colours, falling back to the pane's own so a
  // line asking only for inverse still reads as inverted.
  const fg = style.inverse ? (style.bg ?? 'var(--color-surface-1)') : style.fg
  const bg = style.inverse ? (style.fg ?? 'var(--color-ink)') : style.bg

  if (fg) segment.fg = fg
  if (bg) segment.bg = bg
  if (style.bold) segment.bold = true
  if (style.dim) segment.dim = true
  if (style.italic) segment.italic = true
  if (style.underline) segment.underline = true

  return segment
}

/** parseAnsi splits one line into the runs of text the viewer renders. */
export function parseAnsi(line: string): Segment[] {
  const segments: Segment[] = []
  let style: Style = {}
  let cursor = 0

  sequence.lastIndex = 0
  for (let hit = sequence.exec(line); hit; hit = sequence.exec(line)) {
    if (hit.index > cursor) segments.push(cut(style, line.slice(cursor, hit.index)))
    cursor = sequence.lastIndex

    // Only the SGR alternative captures a group. Everything else matched
    // purely so that it would not be rendered.
    if (hit[1] !== undefined) style = apply(style, hit[1])
  }

  if (cursor < line.length) segments.push(cut(style, line.slice(cursor)))
  return segments
}

/** stripAnsi is the plain text of a line, for searching and for copying. */
export function stripAnsi(line: string): string {
  return line.replace(sequence, '')
}

/**
 * highlight marks the runs matching a search term.
 *
 * The term is found within each segment rather than across the whole line
 * because a match that straddles a colour change would have to be split for
 * rendering anyway, and a colour change inside a word is vanishingly rare.
 */
export function highlight(segments: Segment[], needle: string, matchCase: boolean): Segment[] {
  if (!needle) return segments

  const term = matchCase ? needle : needle.toLowerCase()
  const marked: Segment[] = []

  for (const segment of segments) {
    const haystack = matchCase ? segment.text : segment.text.toLowerCase()
    let from = 0

    for (let at = haystack.indexOf(term); at !== -1; at = haystack.indexOf(term, from)) {
      if (at > from) marked.push({ ...segment, text: segment.text.slice(from, at) })
      marked.push({ ...segment, text: segment.text.slice(at, at + term.length), match: true })
      from = at + term.length
    }

    if (from === 0) marked.push(segment)
    else if (from < segment.text.length) marked.push({ ...segment, text: segment.text.slice(from) })
  }

  return marked
}
