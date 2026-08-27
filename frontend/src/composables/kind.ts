import { Kind } from '@/types'
import type { KindInfo } from '@/types'

const builtIn = new Set<string>(Object.values(Kind))

/**
 * asKind narrows a route parameter to a resource kind the cluster serves.
 *
 * The URL is user-editable, so `/r/wigets/default/x` is reachable. Returning
 * undefined lets the caller say so, instead of sending a nonsense kind to Go
 * and rendering its error as though the cluster were at fault.
 *
 * The cluster's own catalogue decides, not the list compiled into this file. A
 * custom resource is named `plural.group` — deliberately, so it cannot collide
 * with a built-in kind — and it exists only because somebody's operator
 * installed it, so nothing shipped with this application can know it. Checking
 * against the compiled-in list is what made every CRD in every cluster look
 * like a typo.
 */
export function asKind(value: string, catalogue: KindInfo[] = []): Kind | undefined {
  if (!value || value === Kind.$zero) return undefined
  if (catalogue.length) {
    return catalogue.some((entry) => entry.kind === value) ? (value as Kind) : undefined
  }
  // Before the catalogue arrives — a deep link opening into a cluster that is
  // still connecting — a built-in kind is already known to be real.
  return builtIn.has(value) ? (value as Kind) : undefined
}

/** singularTitle turns catalogue plurals into the inspector heading. */
export function singularTitle(title: string): string {
  if (title.endsWith('ies')) return `${title.slice(0, -3)}y`
  if (title.endsWith('ses')) return title.slice(0, -2)
  if (title.endsWith('s')) return title.slice(0, -1)
  return title
}
