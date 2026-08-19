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
