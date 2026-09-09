# Incident Workspace — Progress

อัปเดตล่าสุด: 2026-09-09

## ฟีเจอร์ปัจจุบัน

| Work package | สถานะ | หมายเหตุ |
|---|---|---|
| IW-00 Baseline/fixtures | ✅ เสร็จ | `internal/testfixture/` + baseline tests |
| IW-01 CRD discovery fallback | 🚧 รอ release gate | implementation + tests ผ่าน locally |
| IW-02 Read-only policy | ⏸ รอ IW-01 release | |

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

### ผลทดสอบ (local, 2026-09-09)

```text
go test -race ./internal/kube ./internal/cluster ./internal/resources ./internal/testfixture  → PASS
go build -tags production .                                                                    → PASS
npm --prefix frontend run build                                                                → PASS
```

ข้อจำกัด:

- ยังไม่ได้ทดสอบบน disposable cluster จริง (RBAC จำกัด namespace)
- Release CI (macOS codesign/notarize + Windows NSIS) ยังไม่รันในรอบนี้

### Commit / PR

| รายการ | ค่า |
|---|---|
| Branch | `feat/iw-01-discovery-fallback` |
| Commit | `533e3fb9c90766baceca18669a1b7b704c5579dc` |
| PR | ⏸ push ถูก deny (`napisoot-ttss` → `ncmink/biebie-kube`); ต้อง push/เปิด PR ด้วย account ที่มีสิทธิ์ |
| Version | 0.2.11 |

### Published artifacts & smoke test

| รายการ | สถานะ |
|---|---|
| GitHub Release | ⏸ รอ tag `v0.2.11` + CI |
| macOS `.dmg` / updater zip | ⏸ |
| Windows installer / updater zip | ⏸ |
| Smoke test บน artifact ที่เผยแพร่ | ⏸ |

## ฟีเจอร์ถัดไป (หลัง release gate ผ่าน)

**IW-02 Read-only policy** (`POL-*`)

## Baseline (IW-00)

Fixtures ใน `internal/testfixture/incident.go` สำหรับ discovery scenarios และ pod states ที่ IW-03 จะ reuse
