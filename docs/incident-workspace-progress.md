# Incident Workspace — Progress

อัปเดตล่าสุด: 2026-10-06

## ฟีเจอร์ปัจจุบัน

| Work package | สถานะ | หมายเหตุ |
|---|---|---|
| IW-00 Baseline/fixtures | ✅ เสร็จ | `internal/testfixture/` + baseline tests |
| IW-01 CRD discovery fallback | ✅ เสร็จ | released `v0.2.11` (`60c7c03`) |
| IW-02 Read-only policy | ✅ เสร็จ | released `v0.2.12` (`7e3df2b`) |
| IW-03 Explain Why | ✅ เสร็จ | released `v0.2.13` (`abedfc7`) |
| IW-04 Typed filters | ✅ เสร็จ | released `v0.2.15` (`9b87039`) |
| IW-07 Saved views | ✅ เสร็จ | released `v0.2.16` |

## สิ่งที่โชว์บน biebie.net

หน้า `/biebie-kube` อธิบายฟีเจอร์ Incident Workspace ที่ ship แล้ว:

| ฟีเจอร์ | Work package | หมายเหตุ marketing |
|---|---|---|
| Custom resources without CRD list | IW-01 | ขยายจาก “Around forty resource kinds” |
| Read-only mode per cluster | IW-02 | + FAQ เรื่อง investigation โดยไม่ mutate |
| Explain why a workload is stuck | IW-03 | findings + next steps |
| Typed filters and selectors | IW-04 | expression + label/field + filter builder |
| Saved views | IW-07 | command palette + resource list |

## IW-04 — Typed filters

Released `v0.2.15` (`9b87039`). Expression filters (`v0.2.14`), selectors + `ResourceFilterBuilder` UX (`v0.2.15`).

## IW-07 — Saved views

Released `v0.2.16`.

### Requirement IDs

| ID | สถานะ | หลักฐาน |
|---|---|---|
| VIEW-01 | ✅ | Save/update/delete named query + columns ใน `internal/views/` |
| VIEW-02 | ✅ | เก็บใน local `data.json` เท่านั้น ไม่มี live rows |
| VIEW-03 | ✅ | Command palette + resource list; cluster switch explicit พร้อม cluster id |
| VIEW-04 | ✅ | `ResolveSavedView` → unresolved issues (kind/namespace/column/query) |
| VIEW-05 | ✅ | `queryVersion=1`, limit 100/cluster, title ≤80 chars |

### API / UI

| รายการ | ไฟล์ |
|---|---|
| Persistence | `internal/store/store.go` (`SavedViewRecord`, schema v2) |
| Repository + resolve | `internal/views/` |
| Wails API | `service_cluster.go` (`List/Save/Delete/ResolveSavedView`) |
| Store | `frontend/src/stores/savedViews.ts`, `stores/resources.ts` |
| UI | `SavedViewControls.vue`, `CommandPalette.vue` |

### ผลทดสอบ (release gate, 2026-09-16)

```text
go test ./internal/views/... ./internal/resources/...  → PASS
npm --prefix frontend run build                       → PASS
go build -tags production -o bin/biebie-kube .        → PASS
```

### Dev/prod isolation (`v0.2.16`)

Dev build ใช้ bundle id / single-instance / state path แยกจาก production (`biebie-kube-dev`) เพื่อไม่ให้ schema ใหม่ใน dev ทำให้ prod เปิดไม่ขึ้น

## Explain Why — node network (2026-10-06)

Explain Why รองรับ Kind Node แล้ว กฎอ่านเฉพาะข้อมูลที่ Kubernetes API เปิดให้ และไม่ผูกกับชื่อ CNI

| RuleID | ความรุนแรง | เงื่อนไข |
|---|---|---|
| `node.not_ready` | critical, confirmed | Ready เป็น False หรือ Unknown |
| `node.network_plugin_not_ready` | critical, confirmed | ข้อความ Ready มี NetworkPluginNotReady / network plugin / cni หรือ NetworkUnavailable=True |
| `event.pod_sandbox_failed` | critical, supported | Warning FailedCreatePodSandBox |
| `node.network_resource_exhaustion` | warning, supported | network plugin หรือ sandbox ล้ม และข้อความเป็น allocation failure และ MemoryPressure ไม่ใช่ True |

ความหนาแน่นของ pod (เช่น เกิน 80% ของ maxPods) เป็น fact เสริมเท่านั้น ไม่ทำให้ heuristic ทำงานเอง

การเก็บข้อมูลใช้ field selector (`spec.nodeName`, `involvedObject.kind=Node`) และจำกัดจำนวน pod ที่อ่าน event ไว้ที่ 10 ถ้า list/get ถูก forbidden จะได้ coverage แบบ partial พร้อม issue และยังสรุปจาก condition ที่อ่านได้

RBAC ที่ใช้เพิ่มคือการอ่านอย่างเดียว: `get` nodes, `list` pods ทั้ง cluster รวม kube-system, `list` events

### นอกขอบเขต

- อ่าน `/proc/user_beancounters` หรือ `/proc` บน node
- สร้าง pod, exec, SSH, หรือ debug pod
- Prometheus และการแจ้งเตือนต่อเนื่อง
- ตรวจจับ OpenVZ โดยเฉพาะ นอกจากข้อความใน NextSteps

## ฟีเจอร์ถัดไป

**IW-05 Session timeline** (`TML-*`)

**IW-06 Workload log groups** (`LOG-*`)

## Baseline (IW-00)

Fixtures ใน `internal/testfixture/incident.go` สำหรับ discovery scenarios และ pod states
