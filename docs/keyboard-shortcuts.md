# Keyboard shortcuts

This document defines Biebie Kube keyboard shortcuts, with **Quick Navigation**
inspired by [TablePlus left-sidebar quick navigation](https://docs.tableplus.com/gui-tools/the-interface/left-sidebar#quick-navigation).

**Status:** implemented in the Vue frontend (`composables/useGlobalShortcuts.ts`,
`useResourceListShortcuts.ts`, `useResourceDetailShortcuts.ts`,
`components/common/NavigationSwitchers.vue`, `ShortcutCheatSheet.vue`). See
[Implementation notes](#implementation-notes-follow-up) for registry follow-ups.

## UI mapping (TablePlus → Biebie Kube)

| TablePlus concept | Biebie Kube surface |
| --- | --- |
| Connection | Open cluster tab (`ClusterTabs.vue`) |
| Database | Selected namespace (`NamespaceSelector.vue` on the left sidebar) |
| Tables / views in the sidebar | Resource kinds (`ResourceNav.vue`) |
| Query editor | Full resource workspace — **Logs** and **Terminal** (`ResourceDetail.vue`), opened from the drawer via “Open logs and terminal” |
| Fuzzy search in the sidebar | Command palette (`CommandPalette.vue`) — clusters, namespaces, kinds, saved views, and live resource search |

## Notation

| Symbol | macOS | Windows / Linux |
| --- | --- | --- |
| `⌘` | Command (`metaKey`) | Control (`ctrlKey`) |
| `⇧` | Shift | Shift |
| `⌥` | Option | Alt |
| `⏎` | Return | Enter |

In prose below, **⌘** means “use Command on macOS and Control elsewhere”, matching
the existing command palette binding.

## Principles

1. **One mental model for switchers** — Cluster, namespace, and resource-kind
   pickers share the same interaction: type to filter, `↑` / `↓` to move,
   `⏎` to confirm, `Esc` to dismiss.
2. **Command palette stays universal** — `⌘K` remains the fuzzy “find anything”
   entry point (clusters, namespaces, kinds, saved views, resources by name).
   TablePlus uses `⌘K` for databases; Biebie Kube keeps `⌘K` for the palette
   and assigns namespace switching to `⌘⇧N` so we do not break the shipped
   shortcut or the TitleBar hint.
3. **Do not fight the OS** — Avoid rebinding `⌘Q`, `⌘H`, `⌘M`, clipboard
   shortcuts, `⌘F` (find), and `⌘R` (reload) inside the webview.
4. **Context-aware bare keys** — Single-key bindings (`/`, `j`, `k`) apply only
   when focus is not in an `<input>`, `<textarea>`, Monaco, or xterm.
5. **One overlay at a time** — While the command palette, a switcher, a confirm
   dialog, or the shortcut cheat sheet is open, other global shortcuts that open
   new overlays are ignored.
6. **Connected cluster required** — Namespace and resource-kind switchers, and
   resource-list navigation shortcuts, are inactive until the active cluster
   session is `connected`.

## Quick navigation

These are the primary “jump without reaching for the mouse” actions, aligned
with TablePlus quick navigation where it makes sense.

| Action | Keys | When it applies |
| --- | --- | --- |
| Command palette (search everything) | `⌘K` | Global |
| Switch cluster (open tabs first) | `⌘⇧K` | Global; lists `clusters.openClusters`, then pinned clusters from the home screen |
| Switch namespace | `⌘⇧N` | Active cluster connected; same list as `NamespaceSelector` |
| Go to resource kind | `⌘P` | Active cluster connected; fuzzy list from the catalogue (Pods, Deployments, CRDs, …) |
| Open logs and terminal | `⌘⏎` | Resource list with a selected row, or resource drawer open — navigates to `resource` with `?tab=Logs` (same as the drawer button) |

### TablePlus reference

| TablePlus | Biebie Kube |
| --- | --- |
| Show Connections `⌘⇧K` | Switch cluster `⌘⇧K` |
| Show Databases `⌘K` | Switch namespace `⌘⇧N` (palette keeps `⌘K`) |
| Sidebar fuzzy search | Command palette `⌘K` |
| Open Query Editor `⌘⏎` | Open logs and terminal `⌘⏎` |
| (no direct equivalent) | Go to resource kind `⌘P` |

## Cluster tabs

Multiple open clusters behave like TablePlus connections: each tab owns its
session, namespace, watches, and port forwards on the Go side.

| Action | Keys |
| --- | --- |
| Focus cluster tab 1–8 | `⌘1` … `⌘8` |
| Focus last open cluster tab | `⌘9` |
| Next / previous cluster tab | `⌘⇧]` / `⌘⇧[` |
| Close active cluster tab | `⌘W` (see [Native menu and OS shortcuts](#native-menu-and-os-shortcuts)) |

Closing the last cluster tab returns to the clusters home (`Dashboard.vue`), same
as clicking × on the tab today.

## Global navigation

| Action | Keys |
| --- | --- |
| Clusters home | `⌘⇧H` |
| Cluster overview | `⌘0` (when a cluster route is active) |
| Port forwards | `⌘⇧F` |
| Settings | `⌘,` |
| Keyboard shortcut cheat sheet | `⌘/` |

## Resource list

Applies on `ResourceList.vue` when the cluster is connected.

| Action | Keys |
| --- | --- |
| Focus filter / expression field | `/` |
| Move selection up / down | `↑` / `↓` or `j` / `k` |
| Open right-hand drawer (inspect) | `⏎` |
| Close drawer | `Esc` |
| Open full detail → Logs tab | `⌘⏎` |
| Refresh list | `⌘⇧R` |
| Delete selected resource | `⌘⌫` — opens existing `ConfirmDialog`; production clusters still require typing the name |

Selection model: one highlighted row (distinct from checkbox multi-select if
present). If nothing is selected, `⏎` / `⌘⏎` do nothing.

## Resource detail

Applies on `ResourceDetail.vue`.

| Action | Keys |
| --- | --- |
| Switch tab by index | `⌘⌥1` … `⌘⌥6` |

Tab index follows the visible tab strip for the current kind:

| Index | Pod | Other explainable kinds | Default kinds |
| --- | --- | --- | --- |
| 1 | Overview | YAML | YAML |
| 2 | Logs | Events | Events |
| 3 | Terminal | Explain | — |
| 4 | YAML | — | — |
| 5 | Events | — | — |
| 6 | Explain | — | — |

If a slot has no tab for the current kind, the shortcut is a no-op.

| Action | Keys |
| --- | --- |
| Back to resource list | `Esc` when no modal is open and focus is not in Monaco/xterm |

## Context menus and overlays

| Action | Keys |
| --- | --- |
| Close context menu / dropdown / drawer | `Esc` — **partially implemented** per component |
| Command palette: move selection | `↑` / `↓` |
| Command palette: run | `⏎` |
| Command palette: close | `Esc` |

## Scope and conflict rules

### Focus guards

Bare-key shortcuts must check `event.target` (or a shared `isTypingTarget()`
helper) and bail when the active element is:

- an input or textarea (including the command palette search field),
- Monaco (`YamlEditor`),
- xterm (`PodTerminal`, `LogViewer` when focused).

### Modifier shortcuts

`⌘` shortcuts should run in the capture phase on `window` so they still work
when a child control has focus, except:

- **Monaco** — `⌘K` starts a chord in the editor. The app should listen for
  `⌘K` without a follow-up key within ~300 ms and treat that as “toggle command
  palette”, and not steal Monaco’s `⌘K` chords when a second key arrives.
- **xterm** — Do not register handlers that prevent `⌘C` / `⌘V` copy/paste in
  the terminal.

### Palette and switcher mutual exclusion

Opening any of the following closes the others:

- command palette (`ui.paletteOpen`),
- cluster / namespace / kind switcher (proposed `ui.switcher`),
- shortcut cheat sheet.

### Search vs switcher

- **Command palette** — cross-cutting actions and resource name search (API).
- **`⌘P`** — fast jump to a **kind** in the sidebar catalogue only (no cluster
  list, no settings).

## Discoverability

1. **Cheat sheet** — `⌘/` opens a modal grouped by Quick navigation / Tabs /
   List / Detail / Global, generated from the shortcut registry (see below).
2. **TitleBar** — Keep the `⌘K` hint on the palette button; add subtle hints on
   namespace control (“`⌘⇧N`”) when connected.
3. **Switcher footers** — Each switcher shows `↑↓` move · `⏎` select · `Esc`
   close.

## Native menu and OS shortcuts

Findings from the current Wails entrypoint ([`main.go`](../main.go)):

- Biebie Kube **does not** register a custom `application.Menu` or Go-side
  accelerators. Keyboard handling is entirely in the Vue frontend today.
- **macOS** still provides the standard application menu (Quit, Close Window,
  Edit, etc.) from the system / Wails defaults.
- **`⌘W`** — On macOS this is conventionally **Close Window**. For a single-main-
  window app with `ApplicationShouldTerminateAfterLastWindowClosed: true`, the
  system may close the app instead of only closing a cluster tab. **Mitigation
  options for implementation:**
  1. Prefer `⌘W` for “close cluster tab” only after confirming the webview
     receives the key before the menu (capture-phase handler + `preventDefault`
     when at least one cluster tab is open).
  2. If the menu always wins, document and bind **close cluster tab** to
     `⌘⌥W` instead, and list both in the cheat sheet until behaviour is verified
     on macOS, Windows, and Linux builds.
- **`⌘⇧N`** — Not used in `main.go`. On some browsers `⌘⇧N` opens a private
  window; that does not apply inside Biebie Kube’s webview. Safe to use for
  namespace switcher in the desktop app.
- **`⌘P`** — Not registered natively. Historically “Print” in some contexts; in
  a desktop webview it is available for “go to kind” if not bound by the host.

Re-check this section when adding a custom Wails menu (e.g. File → Close Tab)
so Go accelerators and frontend shortcuts stay in sync.

## Implementation notes (follow-up)

### Shipped in the frontend

- Global shortcuts: `composables/useGlobalShortcuts.ts` (capture phase on `window`)
- List shortcuts: `composables/useResourceListShortcuts.ts` in `ResourceList.vue`
- Detail tab shortcuts: `composables/useResourceDetailShortcuts.ts` in `ResourceDetail.vue`
- Switchers: `components/common/NavigationSwitchers.vue` + `QuickSwitcher.vue`
- Cheat sheet: `components/common/ShortcutCheatSheet.vue` (`⌘/`)
- UI overlay state: `stores/ui.ts` (`switcher`, `shortcutsOpen`, `openPalette`, …)

### Possible follow-up

### Central registry

Replace scattered `window.addEventListener('keydown', …)` calls with a single
module, e.g. `frontend/src/composables/shortcuts.ts`:

```ts
type ShortcutScope = 'global' | 'cluster' | 'resourceList' | 'resourceDetail'

interface Shortcut {
  id: string
  label: string
  keys: string // display form, e.g. '⌘⇧K'
  match: (event: KeyboardEvent) => boolean
  scope: ShortcutScope
  when?: () => boolean
  run: (event: KeyboardEvent) => void
}
```

- Register shortcuts once from `App.vue` (or a `useShortcuts()` plugin).
- Export the list for the cheat sheet UI (`⌘/`).
- Unit-test: no two shortcuts in the same scope may `match` the same key
  chord when their `when()` predicates can both be true.

### UI store

Extend [`frontend/src/stores/ui.ts`](../frontend/src/stores/ui.ts):

```ts
switcher: null | 'cluster' | 'namespace' | 'kind'
shortcutsOpen: boolean
```

Opening the palette sets `switcher` to `null`; opening a switcher sets
`paletteOpen` to false.

### Switcher components

Implement three small modals reusing the interaction pattern from
[`MenuSelect.vue`](../frontend/src/components/common/MenuSelect.vue) and
[`NamespaceSelector.vue`](../frontend/src/components/cluster/NamespaceSelector.vue):

- **Cluster** — `clusters.openClusters`, then optionally “Open another…” → home.
- **Namespace** — `clusters.namespaces[clusterId]` with fuzzy filter.
- **Kind** — `clusters.catalogues[clusterId]` grouped by category; Argo CD and
  “Port Forwarding” entries mirror `ResourceNav.vue`.

### Resource list selection

Add a `selectedKey` (or reuse table focus state) in
[`ResourceList.vue`](../frontend/src/views/ResourceList.vue) so `⏎`, `⌘⏎`, and
`⌘⌫` have a stable target without requiring a mouse click first.

### Already implemented

| Shortcut | Location |
| --- | --- |
| `⌘K` toggle command palette | `frontend/src/App.vue`, hint in `TitleBar.vue` |
| `Esc` close drawer | `ResourceDrawer.vue` |
| `Esc` close log expanded view | `LogViewer.vue` |
| `Esc` close menus / namespace dropdown | `ContextMenu.vue`, `MenuSelect.vue`, `NamespaceSelector.vue` |

## Changelog

| Date | Change |
| --- | --- |
| 2026-10-09 | Initial TablePlus-style quick navigation design; native menu survey recorded |
| 2026-10-09 | Frontend implementation of shortcuts, switchers, and cheat sheet |
