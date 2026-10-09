# Screen space on resource lists

Resource list pages stacked many horizontal chrome rows before the table.
Each row is only ~34–49px tall, but together they consumed roughly **250px**
on a typical laptop — about a quarter of the viewport before the first data row.

## What we changed

### Session read-only control (~34px saved in the common case)

**Before:** [`AccessModeBar.vue`](../frontend/src/components/cluster/AccessModeBar.vue)
rendered a full-width bar under cluster tabs even when the cluster was
read-write, solely to host **Make this session read-only** on the right.

**After:**

- The session toggle lives on the right of [`ClusterTabs.vue`](../frontend/src/components/cluster/ClusterTabs.vue) as a compact lock control (visible when the active cluster is connected and session read-only can be toggled).
- `AccessModeBar` renders only when the effective mode is **read-only**, showing the warning copy and **Allow writes this session** when applicable.

### Saved views (~41px saved)

**Before:** [`SavedViewControls.vue`](../frontend/src/components/resource/SavedViewControls.vue)
occupied a dedicated row under the filter builder, including empty-state copy.

**After:**

- A **Views** dropdown on the resource list header (beside Refresh / Create) lists saved views for the current kind, with Edit / Delete actions and **Save current view…**.
- The save/edit dialog is unchanged; only the always-on strip was removed.

## Approximate vertical budget

| Chrome row | Typical height | Notes |
| --- | --- | --- |
| TitleBar | ~48px | Unchanged |
| ClusterTabs | ~36px | Session lock added here (no extra row) |
| AccessModeBar | 0 or ~34px | Row removed when read-write |
| Resource list header | ~49px | Views dropdown added inline |
| ResourceFilterBuilder | ~45px | Unchanged |
| Saved views strip | **removed** | Was ~41px |
| Table header | ~34px | Unchanged |

**Net saving:** ~75px when read-write and no saved-view strip — about **two to three**
extra table rows at the default row height (~34px).

## Rationale

- **Progressive disclosure:** Session read-only and saved views are infrequent actions; they do not need permanent full-width rows.
- **Warning when it matters:** The read-only banner stays prominent once the session or cluster is actually read-only.
- **Saved views stay discoverable:** Count badge on **Views** and the command palette still list saved views cluster-wide.
