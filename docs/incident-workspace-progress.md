# Incident Workspace — Progress

อัปเดตล่าสุด: 2026-09-09

## ฟีเจอร์ปัจจุบัน

| Work package | สถานะ | หมายเหตุ |
|---|---|---|
| IW-00 Baseline/fixtures | ✅ เสร็จ | `internal/testfixture/` + baseline tests |
| IW-01 CRD discovery fallback | ✅ เสร็จ | released `v0.2.11` (`60c7c03`) |
| IW-02 Read-only policy | 🚧 รอ release gate | implementation + tests ผ่าน locally |

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

### ผลทดสอบ (local, 2026-09-09)

```text
go test -race ./internal/policy ./internal/cluster/...  → PASS
go build -tags production .                             → PASS
wails3 generate bindings -clean=true -ts -i             → PASS
npm --prefix frontend run build                         → PASS
```

### Version

`0.2.12`

## ฟีเจอร์ถัดไป (หลัง release gate ผ่าน)

**IW-03 Explain Why** (`EXP-*`)

## Baseline (IW-00)

Fixtures ใน `internal/testfixture/incident.go` สำหรับ discovery scenarios และ pod states ที่ IW-03 จะ reuse
