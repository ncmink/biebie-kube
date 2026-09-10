# Biebie Kube: Incident Workspace — Implementation Plan

สถานะ: In progress — IW-00 baseline และ IW-01 discovery fallback implemented locally; release gate pending

วันที่: 2026-09-09 · Baseline: `e643c03` · Target: `biebie-kube`

ข้อกำหนดอ้างอิง: [Product & Technical Spec](incident-workspace-spec.md)

## 1. ผลลัพธ์ที่ต้องการ

ช่วยให้ DevOps/SRE ที่ดูแลหลาย customer ไปจาก “resource มีปัญหา” ถึง
“เข้าใจว่าเกิดอะไรขึ้น เห็นหลักฐานที่รองรับ และรู้ว่าจะตรวจอะไรต่อ”
ได้ใน workspace เดียว โดยต่อยอดระบบ client-go, watch/cache, Argo CD และ GitOps ที่มีอยู่

ลำดับความสำคัญ:

1. เปิด resource ที่ผู้ใช้มีสิทธิ์เข้าถึงได้ และควบคุมการเขียนจากแอปได้ชัดเจน
2. เพิ่ม Explain Why เพื่อแปลง resource state และ related evidence
   เป็นคำอธิบายที่ตรวจสอบย้อนกลับได้
3. เพิ่ม typed filters และ selectors เพื่อค้นหา resource ที่ต้องตรวจได้เร็วขึ้น
4. เพิ่มประวัติที่แอปสังเกตเห็น และ logs รวมตาม workload
5. เพิ่ม Helm inspector และ workflow extensions ตามการใช้งานจริง

แผนนี้ไม่เปลี่ยน `biebie-protocol`, ไม่แก้ VPN/SSH implementation ใน Biebie Access,
ไม่เปลี่ยน stack และไม่สร้าง resource pipeline หรือ Argo integration ขึ้นใหม่

## 2. Baseline ที่ตรวจแล้ว

- CRD browser อ่าน `additionalPrinterColumns` และ health conventions แล้ว
  แต่ custom catalogue ยังขึ้นกับสิทธิ์ list CRD ระดับ cluster
- Informer/cache มี row delta, debounce, watch idle eviction และ watch count cap;
  ตาราง Vue มี virtualization อยู่แล้ว
- มี metrics และ sorting; text filter ปัจจุบันค้นเฉพาะ name fragment
- มี pod/container diagnostics, events และ related resources แต่ยังไม่มี
  incident report ที่รวมหลักฐานเป็นคำอธิบายเดียว
- มี Argo activity จาก Kubernetes Events แต่ยังไม่มี generic session state timeline
- มี log streams แบบ bounded แต่หนึ่ง request ระบุหนึ่ง pod/container
- มี production confirmations และ GitOps write gates แต่ยังไม่มี read-only
  policy ที่ครอบ service paths ทั้งหมด
- มี Argo sync/refresh/batch และ Source vs Live; Helm release inspector,
  Flux actions และ saved resource queries ยังไม่พบใน source ที่ตรวจ

ข้อสรุปนี้มาจาก source review ไม่ใช่ผลทดสอบบน live cluster

## 3. ระยะและเงื่อนไขส่งมอบ

### Phase 0 — บันทึก baseline และเตรียม fixtures

ขอบเขต: `IW-00`

- บันทึก existing behavior ของ CRD restricted RBAC, filters, resource actions,
  logs และ disconnect โดยใช้ fake clients/fixtures ก่อน
- จัด test fixtures สำหรับ CrashLoopBackOff, OOMKilled, ImagePullBackOff,
  FailedScheduling, PVC pending, rollout ค้าง และ Events ที่อ่านไม่ได้
- บันทึก benchmark baseline บนเครื่องเดียวกัน: cache 1k/10k rows,
  filter/sort, burst updates, active watch count และ memory หลังปิด views
- จดสภาพแวดล้อม/commit/dataset/จำนวนรอบให้รันทวนได้ ห้ามใช้ benchmark
  ของ Sofka เป็นตัวเลข baseline ของ Biebie Kube

Exit: มี fixtures และผล baseline ที่ใช้ตรวจ regression ได้;
ไม่มีการสร้างหรือเปลี่ยน resource ใน customer cluster เพื่อเตรียมข้อมูล

### Phase 1 — Discovery และ read-only policy

ขอบเขต: `IW-01`, `IW-02` · Spec: `CRD-*`, `POL-*`

**IW-01: Discovery fallback**

- เพิ่ม discovery result ที่ส่ง preferred version, verbs และ partial errors ได้
- Merge built-in catalogue, API discovery และ CRD metadata ตาม GVR โดยไม่ซ้ำ
- ถ้าอ่าน CRD ไม่ได้ ใช้ discovered type กับ Name/Namespace/Age fallback
- แสดง partial discovery และ object-level forbidden เป็นคนละสถานะกับ empty
- เพิ่ม manual refresh catalogue โดยไม่ reconnect ทุก cluster หรือ reset
  resource view ที่ยัง valid

จุดแก้หลัก: `internal/kube/discovery.go`, `internal/kube/crd.go`,
`internal/cluster/catalogue.go`, `internal/cluster/manager.go`,
`internal/domain/resource.go`, `service_cluster.go`, `ResourceNav.vue`

**IW-02: Read-only policy**

- เพิ่ม persisted cluster default และ session read-only switch ตาม precedence ใน spec
- เพิ่ม policy gate กลางที่ application services ใช้ก่อน side effect ทุกเส้นทาง
- ครอบ resource actions, delete/apply/create, Argo sync/refresh, exec/input,
  port-forward และ helper ที่เปิด tunnel โดยอ้อม
- เปลี่ยนเป็น read-only แล้วปิด exec/port-forward ของ cluster นั้นและยกเลิกงาน
  ที่ยังไม่ dispatch; แสดงผลจริงสำหรับคำสั่งที่ API รับไปแล้ว
- แสดง effective mode ใน cluster header, settings และ action dialogs
- รักษา production confirmation, GitOps ownership gates และ Kubernetes RBAC เดิม

จุดแก้หลัก: `internal/domain`, `internal/store`, `internal/cluster/repository.go`,
new `internal/policy`, `core.go`, `service_resource.go`, `service_authoring.go`,
`service_argocd.go`, `service_stream.go`, cluster settings/header และ action UI

Exit: restricted-RBAC CRD เปิดได้ตามสิทธิ์จริง; เรียก binding ตรงก็ข้าม
read-only ไม่ได้; write mode ยังผ่าน existing gates ตามเดิม

### Phase 2 — Explain Why และ typed filters

ขอบเขต: `IW-03`, `IW-04` · Spec: `EXP-*`, `QRY-*`

**IW-03: Explain Why — evidence-based incident report**

เป้าหมายคือเปลี่ยน resource state, related resources, Events และ diagnostics ที่มีอยู่
ให้เป็นคำอธิบายที่ตอบได้ว่า “เกิดอะไรขึ้น เพราะอะไร มีหลักฐานอะไร และควรตรวจอะไรต่อ”
โดยไม่ใช้ AI เป็นแหล่งตัดสินใน increment นี้

หลักการของ Explain Why:

- เป็น deterministic, evidence-based analysis; ไม่ใช่ AI/chat ใน increment นี้
- ทุก finding ต้องตามกลับไปยัง evidence ที่เก็บได้จริง
- แยก observed fact, supported cause และข้อมูลไม่พอออกจากกัน
- ถ้า evidence อ่านไม่ได้, ถูก RBAC ปฏิเสธ, stale หรือเก็บได้ไม่ครบ
  ต้องแสดง coverage gap แทนการเดา
- `Unknown` เป็นผลลัพธ์ที่ถูกต้องได้; ห้ามสร้างคำอธิบายให้มั่นใจเกินหลักฐาน
- missing evidence ไม่ได้แปลว่าไม่มีปัญหา
- finding ต้องไม่อ้างข้อมูลที่ไม่ได้อยู่ใน collected evidence
- ห้ามส่ง logs, Secrets, kubeconfig, resource payload หรือ incident report
  ไป external AI/service

Implementation:

- สร้าง read-only evidence collector โดย reuse resource inspection,
  UID ownership traversal, Events readers, container diagnostics และ related-resource logic ที่มีอยู่
- เขียน pure rules แยกจาก I/O สำหรับอาการใน Phase 0
- เพิ่ม report contract ที่มี findings, evidence references, coverage,
  collected time และ next-step links
- เพิ่ม Explain ใน resource inspector/detail
- render partial report ได้แม้ evidence บางส่วนอ่านไม่ได้
- findings ต้องไม่เปลี่ยน cluster state และต้องไม่ bypass Kubernetes RBAC
- reuse existing diagnostics แทนการสร้าง Kubernetes readers ซ้ำ

Finding อย่างน้อยต้องระบุ:

- symptom / finding
- explanation
- evidence references
- confidence class: `confirmed`, `supported` หรือ `unknown`
- coverage / missing evidence
- next checks

ไม่ใช้ confidence แบบเปอร์เซ็นต์ เช่น `87%` เว้นแต่มี measurement model
ที่นิยามและพิสูจน์ได้จริง สำหรับ deterministic rules ให้ใช้ confidence class เท่านั้น

ตัวอย่าง conceptual output:

```text
Why this resource is unhealthy

OOMKilled                                      Confirmed

The container was terminated after an
out-of-memory condition was reported.

Evidence
✓ Last termination reason: OOMKilled
✓ Exit code: 137
✓ Configured memory limit: 512 MiB
✓ Restart count: 27

Coverage
✓ Pod state
✓ Container state
✓ Events
△ Metrics unavailable

What to inspect next
→ Previous container logs
→ Memory usage
→ Deployment resource limits
```

ถ้าหลักฐานไม่พอ:

```text
Unable to determine a supported cause

Evidence collected
✓ Pod state
△ Events forbidden
△ Metrics unavailable

The available evidence is insufficient to explain
the current failure without guessing.

What to inspect next
→ Request Events read access
→ Inspect previous container logs
```

จุดแก้หลัก: new `internal/incident`, `internal/domain/incident.go`,
`service_resource.go`, `internal/resources/{pod,inspect,related}.go`,
new `frontend/src/components/resource/IncidentPanel.vue`

**IW-04: Typed filters**

- คง text search เดิมและเพิ่ม expression mode ที่แยกชัดเจน
- ทำ parser/typed evaluator ฝั่ง Go สำหรับ status, health, restarts, age,
  CPU และ memory; compile ครั้งเดียวต่อ query
- เพิ่ม missing/stale metric semantics, parse-error position และ count coverage
- Apply filter/sort บน cache ครบ scope ก่อนแบ่ง window; ใช้ token ป้องกัน
  response/row delta จาก query เก่าปะปน
- เพิ่ม label/field selector controls เป็น increment ถัดไปใน work package เดียวกัน;
  selector ต้องมี identity ใน watch/cache และ fallback list path ด้วย

จุดแก้หลัก: new `internal/resources/query`, `internal/domain/resource.go`,
`internal/resources/{table,service,usage}.go`, `internal/kube/informer.go`,
`frontend/src/views/ResourceList.vue`, `frontend/src/stores/resources.ts`

Exit:

- incident scenarios ให้ finding ที่ตามกลับถึง collected evidence ได้
- partial/forbidden/stale evidence ยังสร้าง report ได้โดยแสดง coverage gap
- เมื่อหลักฐานไม่พอ Explain Why ต้องตอบ `unknown` / `insufficient evidence` แทนการเดา
- findings ต้องไม่อ้างข้อมูลที่ evidence collector ไม่ได้เก็บ
- Explain Why ต้องเป็น read-only และไม่ bypass existing RBAC/policy gates
- filter ไม่ให้ผลผิดจาก pagination, units, metric age หรือ stale query events

**Release A: recommended first release**

Phase 0–2 คือขอบเขตรอบแรก:

- discovery fallback
- read-only policy
- Explain Why
- typed filters
- selector controls หลัง watch identity tests ผ่าน

Product flow ของ Release A:

```text
Find the problem
      ↓
Explain Why
      ↓
Show the evidence
      ↓
Know what to inspect next
      ↓
Operate safely
```

ไม่ผูก release นี้กับ timeline, Helm, Flux, AI หรือ generic bulk actions

### Phase 3 — Session context และ workload logs

ขอบเขต: `IW-05`, `IW-06`, `IW-07` · Spec: `TML-*`, `LOG-*`, `VIEW-*`

**IW-05: Session timeline**

- เพิ่ม bounded observation hook ก่อน table debounce/coalescing เพื่อไม่อ้าง
  ว่า table deltas คือประวัติ state transitions ทั้งหมด
- Project เฉพาะ fields ที่จำเป็นและเก็บตาม cluster/session/GVR/UID
- แยก baseline, transition, deletion, resync baseline และ observation gaps
- แสดง retention, coverage และ truncated/dropped indicators
- มี cursor pagination; เปิด timeline ไม่เริ่ม watch ทุก resource ทั้ง cluster

จุดแก้หลัก: `internal/kube/informer.go`, `core.go`, new `internal/timeline`,
`internal/domain/timeline.go`, resource inspector และ session lifecycle

**IW-06: Workload log groups**

- เปิด group จาก workload/service/selected pod โดยใช้ resolver ที่มีอยู่
- ต่อ stream หลาย pod/container พร้อม source identity และ bounded queues
- รองรับ follow, pause, previous และ per-source errors
- ใช้ selector-aware shared watches ดู membership และจับ pod replacement ตาม UID
- ปิด group/view/disconnect แล้วคืน streams, subscriptions และ frontend buffers

จุดแก้หลัก: `internal/logs`, `internal/resources/related.go`,
`internal/domain/sessions.go`, `service_stream.go`, `LogViewer.vue`,
`frontend/src/stores/sessions.ts`

**IW-07: Saved views**

- Persist query mode/text, selectors, namespace, kind, sort และ selected columns
  ภายใต้ cluster ID; command palette เปิด view ได้
- จัดการ missing kind/namespace/permissions โดยไม่เปลี่ยน scope ให้เอง
- ไม่บันทึก live rows, logs, credentials หรือผล query ลง preference store

จุดแก้หลัก: `internal/store`, `service_cluster.go`, resource list และ command palette

Exit: timeline/log groups มี memory bounds และแสดง gaps จริง;
saved views ไม่ข้าม customer/cluster โดยเงียบ

**Release B:** Phase 3 หลัง Release A เสถียร; saved views ส่งก่อน timeline/logs
ได้เมื่อ query contract นิ่ง ไม่จำเป็นต้องรอทั้ง phase พร้อมกัน

### Phase 4 — Helm inspector

ขอบเขต: `IW-08` · Spec: `HELM-*`

- รองรับ Helm 3 releases ที่เก็บใน Kubernetes Secrets ใน namespace ที่เลือกก่อน
- List, revision history, user-supplied values และ stored manifest แบบ read-only
- แยกค่าที่ถูกเก็บมากับ release จาก computed values ที่ยังไม่ได้ render ใหม่
- Decode ฝั่ง Go ด้วย bounds; ไม่ส่ง release blob หรือข้อมูลดิบเข้า renderer อัตโนมัติ
- Values/manifests เปิด raw ได้ด้วยการกระทำชัดเจน; redaction เป็น best effort
  และไม่อ้างว่าสามารถตรวจพบ secret ทุกชนิดใน arbitrary values ได้
- ไม่ติดตั้ง Helm CLI และไม่ทำ rollback/uninstall ใน increment นี้

จุดแก้หลัก: new `internal/helm`, `internal/domain/helm.go`, `service_helm.go`,
navigation/inspector และ Wails service registration

Exit: release ถูกแยกด้วย namespace/name/revision, malformed payload ถูกจำกัด,
ไม่มี raw secret content ใน default UI/events/persistence

### Backlog ที่ยังไม่รวม release commitment

**IW-09: Flux controls** — ทำเมื่อมี Flux environment ที่ต้องใช้จริง

- เพิ่มเฉพาะ kinds/versions ที่ discovery และ controller contract รองรับ
- แยก suspend/resume/reconcile; verify behavior ของ controller version ก่อน implement
- ใช้ policy gate เดียวกับ Argo และไม่สร้าง authentication ชุดใหม่
- ต้องทำ Flux-specific spec เพิ่มก่อน implementation; ห้ามเดา patch จากชื่อ action

**IW-10: Generic bulk actions** — ทำหลัง policy และ per-item action contracts เสถียร

- เริ่มจาก action เดียวต่อชนิด resource เช่น restart deployments
- Preview รายการเป้าหมาย, UID, scope, production confirmation และผลราย object
- Revalidate เป้าหมายตอน dispatch, bounded concurrency, cancellation และ partial success
- ไม่รับประกัน rollback ทั้ง batch; ไม่ขยาย selection ตาม filter ที่เปลี่ยนระหว่างทำงาน
- ต้องทำ action-specific spec เพิ่มก่อน implementation

Plugin framework, automatic remediation และ rewrite ภาษาอยู่นอกแผนนี้

## 4. Dependencies และลำดับ PR

1. `IW-00` baseline/fixtures
2. `IW-01` discovery metadata → catalogue fallback → UI/status
3. `IW-02` policy/store → all service gates/lifecycle → settings/header/actions
4. `IW-03` domain/collector/rules → Explain UI
5. `IW-04` parser/typed projections → table/window contract → expression UI
6. `IW-04` selectors → watch/cache identity → selector UI
7. `IW-07` saved views หลัง query format นิ่ง
8. `IW-05` observation/retention → timeline UI
9. `IW-06` log group service → membership/lifecycle → merged viewer
10. `IW-08` Helm reader → redacted DTO/reveal → inspector

`IW-03` กับ parser ใน `IW-04` ออกแบบได้อย่างอิสระ แต่ไม่จำเป็นต้องใช้หลาย agent
หรือแก้ common files พร้อมกัน ทุก PR ต้อง usable หรือซ่อน unfinished entry point;
ไม่มีปุ่มที่นำผู้ใช้ไปหน้าว่าง/ฟีเจอร์ที่ backend ยังไม่รองรับ

## 5. วิธีตรวจรับ

ทุก work package ต้องระบุ requirement IDs ที่ทำแล้วและแนบหลักฐาน:

- Go unit tests สำหรับ pure logic และ fake client action assertions สำหรับ I/O
- Lifecycle/race tests เมื่อแก้ watches, caches, session policy หรือ streams
- Frontend typecheck/build และ UI smoke ที่เกี่ยวข้องกับ interaction จริง
- Integration scenarios บน disposable cluster เมื่อ fake client จำลอง API behavior
  เช่น RBAC, discovery, selectors หรือ watch reconnect ไม่เพียงพอ
- ผล restricted RBAC, disconnected/stale data, production context และ switching cluster
- ไม่ใช้ screenshot เพียงอย่างเดียวรับรอง backend enforcement หรือ stream cleanup

คำสั่งจาก repository root สำหรับ implementation PR ตามขอบเขตที่แก้:

```sh
go test ./...
go test -race ./internal/kube ./internal/cluster ./internal/resources ./internal/logs
task common:generate:bindings
npm --prefix frontend run build
git diff --check
```

เพิ่ม packages ใหม่ลง targeted race tests เมื่อ implement แล้ว และใช้ desktop
`task build` ก่อนปล่อย release; bindings ต้อง generate จาก Go ไม่เขียนทับด้วยมือ
หากเครื่องขาด Wails/native dependencies ให้บันทึก check ที่ยังรันไม่ได้อย่างชัดเจน

แผนเอกสารนี้ตรวจเฉพาะความสอดคล้องของ requirements/links/diff ไม่จำเป็นต้อง build
หรือเชื่อมต่อ cluster เพื่อรับรองเอกสาร

## 6. Migration, rollout และ rollback

- เพิ่ม persisted fields แบบ backward-readable: existing cluster ที่ไม่มี mode
  ใช้ write ตามพฤติกรรมเดิม; new explicit production import default เป็น read-only
- Production ที่ auto-import และยังจำแนก environment ไม่ได้ใช้ default เดิม;
  UI เสนอปรับ mode เมื่อผู้ใช้ตั้ง environment เป็น production
- ใช้ store atomic write เดิม เพิ่ม fixture ทดสอบ migration และ deep copy ของ fields ใหม่
- สำรอง state ก่อน schema migration ครั้งแรก; บันทึกข้อจำกัด downgrade ว่า old binary
  อาจไม่บังคับ policy และอาจเขียนทับ unknown preference fields
- Session timeline/log buffers อยู่ memory; restart สูญเสียประวัตินั้นตามที่ UI ระบุ
- ไม่เปลี่ยน wire version, credential flow หรือ ownership ของ VPN/SSH
- ปล่อยเป็น release increments ตาม exit criteria; ย้อนฟีเจอร์ UI ได้โดยยังเก็บ backend
  read-only enforcement และข้อมูล mode ที่ผู้ใช้เลือก

## 7. Definition of done

- Requirement IDs ของ increment ผ่านหรือระบุเหตุผลที่เลื่อนไว้ชัดเจน
- Existing Argo/GitOps write guards, native streams และ Access handoff ไม่ regress
- ไม่มี unbounded queue/watch หรือ raw secret payload ใน summary/events
- Loading/empty/forbidden/partial/stale แสดงความหมายต่างกัน
- Explain Why ทุก conclusion ต้องอ้างกลับถึง collected evidence ได้
- missing/forbidden/stale evidence ต้องลด certainty หรือให้ผล `unknown`
  แทนการ infer เกินข้อมูลที่มี
- deterministic findings ต้องแยก `confirmed`, `supported` และ `unknown` ชัดเจน
- Explain Why ต้องทำงานแบบ read-only และไม่สร้าง side effect ต่อ customer cluster
- ไม่มี logs, Secrets, kubeconfig หรือ raw incident evidence ถูกส่งไป external AI/service
- README และ spec อัปเดตเป็นพฤติกรรมที่ส่งมอบจริง พร้อม validation notes
- ไม่มี changes นอก `biebie-kube` ที่ไม่ได้อยู่ใน scope

## 8. สิ่งที่ต้องเลือกหลังมีข้อมูลเพิ่ม

รายการนี้ไม่บล็อก Release A:

- ปรับ performance targets หลังมี `IW-00` baseline; spec กำหนด initial budgets ไว้แล้ว
- Helm storage drivers เพิ่มเติมต้องดู customer usage ก่อนขยายจาก Secrets
- Flux kinds/versions และ generic bulk action แรกต้องมี use case/test environment
- การเก็บ timeline ข้ามการเปิดแอปต้องมี spec เรื่อง retention/privacy เพิ่มต่างหาก
