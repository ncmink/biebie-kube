# Lens parity roadmap

Gap analysis between recent Lens release notes (Sep–Oct 2026) and **biebie-kube**. Status labels:

| Status | Meaning |
|--------|---------|
| **Have** | Shipped or equivalent today |
| **Quick win** | Small, aligned change (this round) |
| **Roadmap** | Valuable but larger or needs design |
| **N/A by design** | Out of product scope or different model |
| **Excluded** | Explicitly not pursuing (user decision) |

Pointers use repo paths under `biebie-kube/`.

---

## Connectivity and session

| Lens item | Status | Notes |
|-----------|--------|-------|
| Resource lists refresh after sleep / wake | **Quick win** | `frontend/src/composables/useResume.ts`, `App.vue`, `Manager.Resume`, `ClusterService.ResumeCluster` |
| Stop reconnect storms when session expires | **Quick win** | `internal/kube/informer.go` watch error handler → `Manager.SuspendForAuthFailure` |
| Reconnect after network back online | **Quick win** | Same resume path (`online` + visibility) |
| Cloud cluster sign-in (GKE / EKS / AKS) | **Excluded** | Biebie Access + kubeconfig (`README.md`); no cloud OAuth |
| AWS / Azure standalone sign-in | **Excluded** | Same as above |

---

## Metrics and observability

| Lens item | Status | Notes |
|-----------|--------|-------|
| metrics-server usage columns | **Have** | `internal/resources/usage.go`, `capacity.go` |
| Auto-detect Prometheus / VictoriaMetrics | **Roadmap** | Would need discovery + alternate metrics path |
| Cost monitoring | **N/A by design** | Not in Observe / Operate / GitOps focus |

---

## Navigation and chrome

| Lens item | Status | Notes |
|-----------|--------|-------|
| Keyboard-driven context menus | **Quick win** | `ContextMenu.vue` (focus return, Home/End); ARIA on dropdown triggers |
| Window menu + ⌘W close tab | **Roadmap** | Tie to `docs/keyboard-shortcuts.md`; Wails native menu still absent |
| Back / forward navigation audit | **Roadmap** | Vue router history vs Lens expectations |
| Namespace multi-select | **N/A by design** | Single namespace in view by intent |
| Wails v3 upgrade (parity with Electron runtime bumps) | **Roadmap** | Track `docs/keyboard-shortcuts.md` Wails beta notes |

---

## Workloads and resources

| Lens item | Status | Notes |
|-----------|--------|-------|
| Priority Classes in catalogue | **Have** | `internal/domain/catalogue.go` |
| Priority Class detail shows pods using it | **Quick win** | `internal/resources/related.go` (`KindPriorityClass`) |
| Pods count column on Priority Class table | **Roadmap** | Needs per-row aggregate on each list paint |
| Pod details / port-forward (Lens-style) | **Have** | `PodDetails.vue`, `ResourceDrawer.vue`, port-forward service |
| Saved views | **Have** | IW-07; `SavedViewControls.vue` |

---

## Terminal and shell

| Lens item | Status | Notes |
|-----------|--------|-------|
| Pod exec terminal | **Have** | `internal/terminal` |
| Local shell / terminal shell arguments | **N/A by design** | No local shell; exec-only |

---

## Extensions and packaging

| Lens item | Status | Notes |
|-----------|--------|-------|
| Extensions / Marketplace | **Excluded** | Not a platform for third-party UI |
| Helm Releases UI | **Excluded** | GitOps via Argo CD path instead |
| AI features in Lens | **Excluded** | — |

---

## Biebie-specific (not Lens)

| Item | Status | Notes |
|------|--------|-------|
| Biebie Access + tunnelled API | **Have** | `Manager.SuspendForAccessDown`, access handoff |
| Read-only session policy (IW-02) | **Have** | `internal/policy`, `ClusterTabs` toggle |
| Incident workspace (IW-05/06) | **Roadmap** | `docs/incident-workspace-progress.md` |
| TablePlus-style shortcuts | **Have** | `docs/keyboard-shortcuts.md` |

---

## This round (quick wins) — implementation map

1. **Resume** — Frontend heartbeat + Go `ServerVersion` probe; failure → existing unreachable / unauthorized flow.
2. **Auth errors** — `SetWatchErrorHandler`: 401 immediately, repeated 403 → unauthorized, hub closed, forwards/terminals stopped via `core.go` hook.
3. **Menu keyboard** — Context menu focus restore; Home/End; `aria-haspopup` / `aria-expanded` on Views and `MenuSelect` triggers.
4. **Priority Class pods** — Bounded cluster-wide pod list filtered by `spec.priorityClassName`; `Truncated` when budget stops early.

Verification: `go test ./internal/...`, `cd frontend && npx vue-tsc --noEmit && npm run build`.
