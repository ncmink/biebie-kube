export interface ShortcutEntry {
  keys: string
  label: string
}

export interface ShortcutGroup {
  title: string
  items: ShortcutEntry[]
}

export const shortcutCatalog: ShortcutGroup[] = [
  {
    title: 'Quick navigation',
    items: [
      { keys: '⌘K', label: 'Command palette' },
      { keys: '⌘⇧K', label: 'Switch cluster' },
      { keys: '⌘⇧N', label: 'Switch namespace' },
      { keys: '⌘P', label: 'Go to resource kind' },
      { keys: '⌘⏎', label: 'Open logs and terminal (list / drawer)' },
    ],
  },
  {
    title: 'Cluster tabs',
    items: [
      { keys: '⌘1 … ⌘8', label: 'Focus cluster tab' },
      { keys: '⌘9', label: 'Focus last cluster tab' },
      { keys: '⌘⇧]', label: 'Next cluster tab' },
      { keys: '⌘⇧[', label: 'Previous cluster tab' },
      { keys: '⌘W', label: 'Close cluster tab' },
      { keys: '⌘⌥W', label: 'Close cluster tab (fallback)' },
    ],
  },
  {
    title: 'Global',
    items: [
      { keys: '⌘⇧H', label: 'Clusters home' },
      { keys: '⌘0', label: 'Cluster overview' },
      { keys: '⌘⇧F', label: 'Port forwards' },
      { keys: '⌘,', label: 'Settings' },
      { keys: '⌘/', label: 'Keyboard shortcuts' },
    ],
  },
  {
    title: 'Resource list',
    items: [
      { keys: '/', label: 'Focus filter' },
      { keys: '↑ ↓ j k', label: 'Move row focus' },
      { keys: '⏎', label: 'Open drawer' },
      { keys: 'Esc', label: 'Close drawer' },
      { keys: '⌘⇧R', label: 'Refresh list' },
      { keys: '⌘⌫', label: 'Delete selected' },
    ],
  },
  {
    title: 'Resource detail',
    items: [
      { keys: '⌘⌥1 … ⌘⌥6', label: 'Switch tab by index' },
    ],
  },
]
