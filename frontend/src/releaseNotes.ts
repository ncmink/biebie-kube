export interface ReleaseHighlight {
  title: string
  detail?: string
}

export interface ReleaseNote {
  version: string
  date: string
  highlights: ReleaseHighlight[]
  fixes?: string[]
}

/** Newest first. Add an entry when shipping a user-visible release. */
export const releaseNotes: ReleaseNote[] = [
  {
    version: '0.2.22',
    date: '2026-03-25',
    highlights: [
      {
        title: "What's new after you update",
        detail:
          'When you install a new version, Biebie Kube shows a short summary of changes. Open it again anytime from Settings.',
      },
    ],
  },
  {
    version: '0.2.21',
    date: '2026-03-24',
    highlights: [
      {
        title: 'GPU capacity on nodes',
        detail:
          'See GPU requests and free accelerators on the cluster overview, in the node inspector, and when a pod cannot schedule for lack of GPU.',
      },
      {
        title: 'Lists recover after sleep',
        detail:
          'After your Mac sleeps or the network returns, open clusters refresh their resource lists automatically.',
      },
      {
        title: 'Expired sessions stop reconnecting',
        detail:
          'When your cluster session ends, Biebie Kube stops retrying in the background and shows a clear reconnect prompt.',
      },
      {
        title: 'Keyboard-friendly menus',
        detail: 'Context menus support Home and End, and focus returns to where you opened them.',
      },
      {
        title: 'Priority Classes show their pods',
        detail: 'Open a Priority Class to see which pods use it.',
      },
    ],
  },
]

function normalizeVersion(v: string): string {
  return v.trim().replace(/^v/i, '')
}

function parseVersion(v: string): [number, number, number] {
  const parts = normalizeVersion(v).split('.')
  const read = (i: number) => {
    const n = parseInt(parts[i] ?? '0', 10)
    return Number.isFinite(n) ? n : 0
  }
  return [read(0), read(1), read(2)]
}

export function compareVersions(a: string, b: string): number {
  const pa = parseVersion(a)
  const pb = parseVersion(b)
  for (let i = 0; i < 3; i++) {
    if (pa[i] < pb[i]) return -1
    if (pa[i] > pb[i]) return 1
  }
  return 0
}

/** Release notes after lastSeen and up to current, newest first. */
export function notesSince(lastSeen: string, current: string): ReleaseNote[] {
  return releaseNotes.filter((note) => {
    if (compareVersions(note.version, current) > 0) return false
    if (!lastSeen) return true
    return compareVersions(note.version, lastSeen) > 0
  })
}

/** Every bundled note, for Settings → What's new. */
export function allReleaseNotes(): ReleaseNote[] {
  return [...releaseNotes]
}
