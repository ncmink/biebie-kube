# Incident Workspace — Progress

อัปเดตล่าสุด: 2026-09-11

## ฟีเจอร์ปัจจุบัน

| Work package | สถานะ | หมายเหตุ |
|---|---|---|
| IW-00 Baseline/fixtures | ✅ เสร็จ | `internal/testfixture/` + baseline tests |
| IW-01 CRD discovery fallback | ✅ เสร็จ | released `v0.2.11` (`60c7c03`) |
| IW-02 Read-only policy | ✅ เสร็จ | released `v0.2.12` (`7e3df2b`) |
| IW-03 Explain Why | ✅ เสร็จ | released `v0.2.13` (`abedfc7`) |
| IW-04 Typed filters | 🚧 รอ release gate | expression `v0.2.14`; selectors locally `v0.2.15` |

## IW-03 — Explain Why

Released `v0.2.13` (`abedfc7`). See git tag for full EXP-* coverage.

## IW-04 — Typed filters

### Requirement IDs

| ID | สถานะ | หลักฐาน |
|---|---|---|
| QRY-01 | ✅ | Text mode คงเดิม; expression mode แยกชัดใน UI |
| QRY-02 | ✅ | `= != > >= < <=`, `AND`/`&&` ใน `internal/resources/query/parse.go` |
| QRY-03 | ✅ | ตัวอย่าง spec (`restarts >= 5 && age < 2h`, `cpu > 500m`, …) compile ได้ |
| QRY-04 | ✅ | Fields v1: name, namespace, status, health, restarts, age, cpu, memory |
| QRY-05 | ✅ | Unknown metrics/values; `missing(field)`; stale >45s → unknown |
| QRY-06 | ✅ | Age จาก `CreatedAt` + shared `EvalContext.Now` |
| QRY-07 | ⏭️ | Active-view metrics refresh policy deferred (reuse 15s TTL) |
| QRY-08 | ✅ | Filter บน cache ครบ scope ก่อน window (`table.orderedLocked`) |
| QRY-09 | ✅ | Label/field selector inputs + Apply scope ใน `ResourceList.vue` |
| QRY-10 | ✅ | `ParseSelectors` + list/read ส่ง selectors ไป API (`table.go`, `service.go`) |
| QRY-11 | ✅ | Watch key รวม label/field; filtered informer factory (`informer.go`) |
| QRY-12 | ✅ | Query token เปลี่ยนเมื่อ mode/expression/selectors/sort เปลี่ยน |
| QRY-13 | ✅ (partial) | `total`, `matched`, `loaded`, `unknown` ใน `ResourcePage` |
| QRY-14 | ✅ | Compile once; cap 2 KiB / 20 terms; debounce 150ms |

### API / UI

| รายการ | ไฟล์ |
|---|---|
| Parser + evaluator | `internal/resources/query/` |
| Selector parse/validate | `internal/resources/query/selectors.go` |
| Table integration | `internal/resources/table.go` |
| Watch identity | `internal/kube/informer.go` |
| `ParseListQuery` | `service_resource.go` |
| Name / Expression toggle | `ResourceList.vue`, `stores/resources.ts` |
| Scope row (label/field) | `ResourceList.vue`, `stores/resources.ts` |
| Expression autocomplete | `composables/querySuggest.ts` |

### UX notes (local)

- Expression: parse-before-apply — draft ไม่ valid ไม่ revert input; table คง query ล่าสุดที่ valid
- Autocomplete: แนะนำ field / `missing(` / `AND` หลังพิมพ์ ≥2 ตัวอักษร
- Selectors: Apply scope ผ่าน `ParseListQuery`; field selector ไม่รองรับ kind → actionable error

### ผลทดสอบ (local, 2026-09-11)

```text
go test ./internal/resources/query/... ./internal/resources/... ./internal/kube/...  → PASS
npm --prefix frontend run build                                                   → PASS
go build -tags production -o bin/biebie-kube .                                    → PASS
```

### Version

- Expression filters: released `v0.2.14`
- Label/field selectors + watch identity: local `0.2.15` (pending tag)

## ฟีเจอร์ถัดไป (หลัง release gate ผ่าน)

**IW-07 Saved views** (`VIEW-*`) — หลัง query contract นิ่ง

## Baseline (IW-00)

Fixtures ใน `internal/testfixture/incident.go` สำหรับ discovery scenarios และ pod states
