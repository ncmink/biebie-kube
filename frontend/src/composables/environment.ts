import { EnvironmentKind } from '@/types'

/**
 * The environments a cluster can be filed under, and the word each is written
 * with.
 *
 * The word is not decoration. A cluster card and the context trail show the
 * environment's stored name rather than its kind, so a name left behind by an
 * earlier edit is a screen that goes on saying "Production" about a cluster
 * now classified as development.
 */
const titles: ReadonlyMap<EnvironmentKind, string> = new Map([
  [EnvironmentKind.EnvironmentUnknown, ''],
  [EnvironmentKind.EnvironmentDevelopment, 'Development'],
  [EnvironmentKind.EnvironmentStaging, 'Staging'],
  [EnvironmentKind.EnvironmentProduction, 'Production'],
])

/** titleFor is how one environment is spelled out. */
export function titleFor(kind: EnvironmentKind): string {
  return titles.get(kind) ?? ''
}

/**
 * kindOf reads an environment kind out of the words stored beside it.
 *
 * It answers "does this word name an environment, and which", so a label that
 * contradicts the kind it sits next to can be recognised as stale rather than
 * trusted. A word naming none of them — "UAT", "Sandbox" — is somebody's own
 * choice and reported as unknown rather than forced into one.
 */
export function kindOf(...words: string[]): EnvironmentKind {
  for (const word of words) {
    const needle = word.trim().toLowerCase()
    if (!needle) continue
    for (const [kind, title] of titles) {
      if (kind && (needle === kind || needle === title.toLowerCase())) return kind
    }
  }
  return EnvironmentKind.EnvironmentUnknown
}
