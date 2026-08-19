import { Kind } from '@/types'

const known = new Set<string>(Object.values(Kind))

/**
 * asKind narrows a route parameter to a resource kind.
 *
 * The URL is user-editable, so `/r/wigets/default/x` is reachable. Returning
 * undefined lets the caller say so, instead of sending a nonsense kind to Go
 * and rendering its error as though the cluster were at fault.
 */
export function asKind(value: string): Kind | undefined {
  return known.has(value) && value !== Kind.$zero ? (value as Kind) : undefined
}

/** singularTitle turns catalogue plurals into the inspector heading. */
export function singularTitle(title: string): string {
  if (title.endsWith('ies')) return `${title.slice(0, -3)}y`
  if (title.endsWith('ses')) return title.slice(0, -2)
  if (title.endsWith('s')) return title.slice(0, -1)
  return title
}
