# Incident Workspace — Progress

อัปเดตล่าสุด: 2026-09-10

## ฟีเจอร์ปัจจุบัน

| Work package | สถานะ | หมายเหตุ |
|---|---|---|
| IW-00 Baseline/fixtures | ✅ เสร็จ | `internal/testfixture/` + baseline tests |
| IW-01 CRD discovery fallback | ✅ เสร็จ | released `v0.2.11` (`60c7c03`) |
| IW-02 Read-only policy | ✅ เสร็จ | released `v0.2.12` (`7e3df2b`) |
| IW-03 Explain Why | 🚧 รอ release gate | implementation + tests ผ่าน locally |

## IW-01 — CRD discovery fallback

### Requirement IDs

| ID | สถานะ | หลักฐาน |
|---|---|---|
| CRD-01 | ✅ | API discovery เป็นฐาน merge ใน `BuildCatalogue` |
| CRD-02 | ✅ | ตัด subresource / non-listable ใน `mergeResourceLists` |
| CRD-03 | ✅ | merge ตาม group/resource + preferred version |
| CRD-04 | ✅ | discovery-only custom kind ไม่มี CRD columns (`discovery_test.go`) |
| CRD-05 | ✅ | partial issues + unverified built-ins เมื่อ discovery ว่าง |
| CRD-06 | ✅ | `ListAccess` forbidden/snapshot/live ใน `resources/service.go` |
| CRD-07 | ✅ | `RefreshResourceCatalogue` + UI Refresh ใน sidebar |

### Published

| รายการ | ค่า |
|---|---|
| Tag | `v0.2.11` |
| Merge | `60c7c03` |

## IW-02 — Read-only policy

### Requirement IDs

| ID | สถานะ | หลักฐาน |
|---|---|---|
| POL-01 | ✅ | `PreferenceRecord.accessMode`; missing field → `read_write` |
| POL-02 | ✅ | production cluster create → default `read_only` (`repository.go`) |
| POL-03 | ✅ | effective = persisted OR session; session ไม่ override persisted |
| POL-04 | ✅ | session override ล้างเมื่อ `Disconnect` |
| POL-05 | ✅ | read paths ไม่ถูก gate |
| POL-06 | ✅ | `requireWrite` ก่อน apply/delete/action/argo/exec/PF/create |
| POL-07 | ✅ | stop/close อนุญาต; synthesize ถูกบล็อกใน read-only |
| POL-08 | ✅ | ไม่แตะ Access-owned forwards |
| POL-09 | ✅ | `internal/policy.Service.Check` กลาง |
| POL-10 | ✅ | revision bump ต่อ policy change |
| POL-11 | ✅ | `onReadOnly` → `StopCluster` + `CloseCluster` terminals |
| POL-12 | ✅ | production/GitOps guards เดิมยังอยู่ |
| POL-13 | ✅ | unknown capability → `unsupported_capability` |

### UI

| รายการ | ไฟล์ |
|---|---|
| Default access mode ใน cluster settings | `ClusterDialog.vue` |
| Session toggle + banner | `AccessModeBar.vue` |
| Tab badge `RO` | `ClusterTabs.vue` |
| Action dialog context | `ResourceActionDialog.vue` |

### Published

| รายการ | ค่า |
|---|---|
| Tag | `v0.2.12` |
| Merge | `7e3df2b` |

## IW-03 — Explain Why

### Requirement IDs

| ID | สถานะ | หลักฐาน |
|---|---|---|
| EXP-01 | ✅ | Pod + Deployment/StatefulSet/DaemonSet/Job/PVC; custom kinds → not implemented message |
| EXP-02 | ✅ | Collector: root object, related pods, container state, events (`internal/incident/service.go`) |
| EXP-03 | ✅ (MVP) | CrashLoopBackOff, OOMKilled, ImagePullBackOff/ErrImagePull, scheduling, volume mount events |
| EXP-04 | ✅ | Findings with severity, summary, explanation, confidenceClass, observedFacts vs possibleCauses |
| EXP-05 | ✅ | Partial/unknown coverage; insufficient-evidence finding; no “healthy” inference |
| EXP-06 | ✅ | No auto log fetch; next steps suggest opening logs |
| EXP-07 | ✅ | Pure Go rules over bounded `EvidenceBundle`; reuses resource readers |
| EXP-08 | ⏭️ | Cache/de-dupe deferred |
| EXP-09 | ⏭️ | Collection budget/deadline deferred |
| EXP-10 | ✅ | Safe excerpts only; no full YAML/logs in report |

### API / UI

| รายการ | ไฟล์ |
|---|---|
| `ExplainResource` | `service_resource.go` |
| Collector + rules | `internal/incident/` |
| Drawer Explain section | `ResourceDrawer.vue` |
| Detail Explain tab | `ResourceDetail.vue`, `IncidentPanel.vue` |

### ผลทดสอบ (local, 2026-09-10)

```text
go test ./internal/incident/...                          → PASS
wails3 generate bindings -clean=true -ts -i              → PASS
npm --prefix frontend run build                          → PASS
go build -tags production -o bin/biebie-kube .           → PASS
```

### Version

`0.2.13`

## ฟีเจอร์ถัดไป (หลัง release gate ผ่าน)

**IW-04 Query / search** (`QRY-*`)

## Baseline (IW-00)

Fixtures ใน `internal/testfixture/incident.go` สำหรับ discovery scenarios และ pod states ที่ IW-03 reuse
