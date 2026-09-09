# Biebie Kube: Incident Workspace — Product & Technical Spec

สถานะ: Proposed · วันที่: 2026-09-09 · Baseline source: `e643c03`

Implementation plan: [incident-workspace-plan.md](incident-workspace-plan.md)

คำว่า MUST/ต้อง คือ acceptance requirement ของ increment ที่ระบุ
ชื่อ DTO, method และ new file ในเอกสารเป็น proposed contracts ไม่ใช่ API ที่มีแล้ว
ตัวเลข budget คือค่าเริ่มต้นสำหรับ implementation/validation ไม่ใช่ผล benchmark

## 1. Product scope

ผู้ใช้หลักคือ DevOps/SRE ที่สลับหลาย customer/environment/cluster และต้อง
วิเคราะห์ incident ผ่าน desktop workspace โดยใช้ kubeconfig ของตนเอง

### User outcomes

- เปิด custom resource ที่ได้รับสิทธิ์แม้ไม่มีสิทธิ์ list CRDs ทั้ง cluster
- เลือก read-only สำหรับการตรวจระบบและเห็น mode ชัดตลอด workflow
- ค้น pods ที่มีอาการหรือ usage ตามเงื่อนไขได้ โดยไม่พลาดแถวที่ยังไม่ render
- อ่านเหตุผลที่ workload ไม่พร้อมพร้อมหลักฐาน ไม่ต้องประกอบจากหลายหน้าด้วยตนเอง
- ดูความเปลี่ยนแปลงที่แอปสังเกตเห็น และ logs ของ workload ที่มีหลาย replica
- บันทึกมุมมองที่ใช้บ่อย และตรวจ Helm release โดยไม่ติดตั้ง CLI เพิ่ม

### Non-goals

- เปลี่ยน stack, Kubernetes client, resource pipeline หรือ Argo implementation ทั้งชุด
- เปลี่ยน `biebie-protocol`, เพิ่ม VPN/SSH ownership ใน Kube หรือแชร์ credentials
- Remote AI, auto-remediation, cluster-wide persistent audit log หรือ historical monitoring
- รับประกันว่า read-only ของแอปจำกัดการใช้ kubectl/แอปอื่นหรือแทน Kubernetes RBAC ได้
- Helm mutations, Flux controls, generic bulk และ plugin framework ใน Release A/B

## 2. Architecture และหลักการร่วม

คงเส้นทาง `Vue → Wails services → Go domain/services → client-go → Kubernetes`
และ `informer → Go cache → Wails delta → Pinia → virtualized Vue table`
ใช้ generated bindings จาก Go และ persistence ผ่าน `internal/store` เดิม

- Cluster identity ใช้ UUID; object identity ที่เก็บ history/evidence ใช้
  cluster/session + group/resource + namespace + UID ไม่ผูกด้วยชื่ออย่างเดียว
- UI ส่ง reference/query; backend validate scope, policy และ identity ใหม่
- Session epoch เปลี่ยนเมื่อ reconnect; async response/delta ต้องมี epoch/token
  เพื่อไม่เขียนข้อมูลเก่าลง session หรือ customer ใหม่
- Error ต้องแยก `forbidden`, `unauthorized`, `unavailable`, `partial`, `invalid_query`,
  `read_only`, `stale_target`, `cancelled`, `unsupported`; adapter แปลง error เดิมได้
- Empty หมายถึง successful complete read ที่ไม่มีผลลัพธ์; unreadable ไม่ใช่ empty
- จำกัด work/buffer พร้อม coverage/dropped indicators; ห้ามตัดข้อมูลแล้วแสดงว่าครบ
- ไม่ใส่ resource bodies, credentials, logs หรือ Secrets ลง app preference store

### Common contract conventions

- `sessionEpoch` เป็น opaque string ที่ Go สร้างต่อ successful connection;
  pending operation ถือ epoch ตอนเริ่มและตรวจอีกครั้งก่อนคืนผล/dispatch
- `coverage` มี state (`complete`, `partial`, `unknown`), scope,
  observedAt, stale และ issues[]; `stale` แยกจาก completeness เพราะข้อมูลที่เคยครบ
  อาจเก่าได้ Issues ระบุ source/resource และ error code โดยไม่มี raw credentials
- Target reference ใช้ existing `ResourceRef` พร้อม expectedUID แยกเมื่อจำเป็น;
  backend resolve GVR จาก catalogue ของ cluster นั้น ไม่เชื่อ GVR จาก UI โดยลำพัง
- Binding timestamps ใช้ RFC 3339 UTC; durations/budgets ใช้ milliseconds
  และ counts ใช้ integer; metric projection ฝั่ง Go เก็บ canonical numeric quantity
- Error result มี stable code, safe message, retryable และ details ที่ผ่าน sanitization;
  field/query errors เพิ่มตำแหน่งได้ แต่ไม่แนบ raw API response body อัตโนมัติ
- DTO slices ใช้ nullable ตาม Go bindings เดิม และ normalize ที่ frontend API seam;
  null/empty payload ต้องไม่ลบ coverage/error ที่บอกว่าข้อมูลยังอ่านไม่สำเร็จ

## 3. CRD discovery fallback — Release A

### Requirements

- **CRD-01:** API discovery เป็นฐานการระบุ resource ที่ server มีให้บริการ;
  CRD metadata เป็น enrichment สำหรับ columns/schema ไม่ใช่เงื่อนไขเดียวของการนำทาง
- **CRD-02:** ตัด subresources จาก navigation และเลือก listable resources;
  การประกาศ verb ใน discovery ไม่เท่ากับผู้ใช้มีสิทธิ์ใช้ verb นั้น
- **CRD-03:** Merge ด้วย group/resource; เลือก server-preferred served version ก่อน
  และใช้ CRD columns ของ version ที่เลือกจริง ถ้าระบุ preferred ไม่ได้ใช้ deterministic
  served-version fallback พร้อม source marker; ไม่ใช้ columns ของ version อื่นเงียบ ๆ
- **CRD-04:** ถ้า list CRD forbidden แต่ API discovery ได้ ให้ Name/Namespace/Age
  columns และ generic health เท่าที่ข้อมูลยืนยันได้; ไม่สร้าง custom action จากชื่อ kind
- **CRD-05:** ถ้าอ่านบาง API group ไม่ได้ยังแสดงส่วนที่อ่านได้ พร้อม diagnostics
  ระบุ group และเหตุผล; รักษา fallback built-ins เดิมเมื่อ discovery ล้มทั้งหมด
  แต่ต้องระบุว่ายังไม่ verified ไม่อ้างว่าทุก kind ใช้งานได้
- **CRD-06:** List/watch forbidden ของ resource ต้องแสดง forbidden ที่ view นั้น
  ถ้า list ได้แต่ watch ไม่ได้ แสดง snapshot พร้อมเวลาและปุ่ม refresh ไม่แสดง live
- **CRD-07:** Manual refresh catalogue รักษา active selection ที่ยัง valid;
  kind หายไปแสดง unavailable พร้อมเก็บ reference ไม่เปลี่ยนไป kind อื่นเอง

### UX / contracts

ใช้ resource navigation เดิม เพิ่ม notice “Some API groups are unavailable”
พร้อมรายละเอียดและ Refresh; discovery-only custom kind ไม่ต้องมี badge รบกวนทุกแถว

`DiscoverySnapshot`: clusterID, sessionEpoch, resources[], issues[], observedAt,
complete. Resource entry มี GVR, kind, namespaced, supportedVerbs,
preferredVersion และ metadataSource (`builtin`, `crd`, `discovery`)

ไม่ทำ SelfSubjectAccessReview ทุกชนิดอัตโนมัติเพื่อซ่อน navigation;
ใช้ผล list/watch จริงและจำกัด retry/backoff

### Acceptance

- Account อ่าน `widgets.example.io` ใน namespace `team-a` ได้ แต่ list CRD ไม่ได้:
  Widget แสดงใน sidebar และเปิดข้อมูลได้โดยไม่ขยายสิทธิ์
- Same plural ต่าง group ไม่ชนกัน; multiple served versions มีหนึ่ง navigation entry
- Group หนึ่งเสียยังเปิด core Pods ได้ และ UI บอกว่า discovery ไม่ครบ
- Watch denied แต่ list allowed ยังอ่าน snapshot ได้; refresh ไม่เปิด duplicate watch

## 4. Read-only policy — Release A

### Mode และ persistence

- **POL-01:** Cluster preference มี `mode: read_write | read_only`; existing records
  ที่ไม่มี field ใช้ `read_write` เพื่อรักษาพฤติกรรมเดิม
- **POL-02:** New cluster ที่ผู้ใช้ระบุ production ชัดเจน default เป็น read-only;
  cluster ที่ยังไม่ทราบ environment ใช้ default เดิมและเสนอ mode เมื่อจัดหมวดภายหลัง
- **POL-03:** Session toggle เพิ่มข้อจำกัดได้: effectiveReadOnly = clusterReadOnly
  OR sessionReadOnly; session ไม่ override persisted read-only ให้เขียนได้
  ต้องเปลี่ยน cluster preference โดยตรงและเห็น customer/environment/cluster ก่อน
- **POL-04:** Session override ล้างเมื่อ disconnect/restart; persisted mode อยู่เดิม
  UI แสดงที่มาของ effective mode และแจ้ง save failure โดยไม่แสดงว่าบันทึกสำเร็จ

### Allowed / denied operations

- **POL-05:** อนุญาต get/list/watch/discovery/metrics, logs, Explain, timeline,
  local YAML compare และ secret reveal ที่ผู้ใช้เปิดเองตาม RBAC เดิม
- **POL-06:** ปฏิเสธ create/apply/delete, scale/restart/cordon/suspend/trigger,
  Argo sync/refresh, exec/attach/input, debug และเริ่ม Kubernetes port-forward
  รวม helper เช่น Open Argo UI ที่สร้าง port-forward โดยอ้อม
  แม้ Argo refresh ดูเป็นการอ่าน แต่เป็น API patch จึงถูกปฏิเสธ
- **POL-07:** อนุญาต stop/close/cancel/cleanup เสมอ; local authoring และ local
  validation อนุญาต แต่ server-side dry-run requests ที่ใช้ mutating verbs ถูกบล็อก
  และ UI ระบุข้อจำกัดแทนการปลอมว่า validate กับ server แล้ว
- **POL-08:** นโยบายนี้ควบคุม operations ที่ Kube เปิด/สั่งเอง; ไม่ตัด VPN/SSH
  หรือ Access-owned forwards และไม่อ้างว่าสามารถหยุด writes ผ่าน external tools ได้

### Enforcement และ lifecycle

- **POL-09:** Go application layer มี guard กลาง `Check(clusterID, capability)`;
  ทุก side-effect path ต้องเรียกก่อน dispatch รวม background helper และ batch item
  การ disable ปุ่มอย่างเดียวไม่ผ่าน requirement
- **POL-10:** เรียก policy ก่อนเริ่มและก่อน dispatch side effect; จัด serialization
  ของ policy change กับ operation start ต่อ cluster ให้ operation ใหม่ไม่ผ่านหลัง
  mode update สำเร็จ งานที่ API รับไปแล้วอาจจบได้และห้ามอ้างว่า rollback แล้ว
- **POL-11:** เปลี่ยนเป็น read-only ให้หยุด active exec sessions/port-forwards
  ที่ Kube เป็นเจ้าของใน cluster นั้น ยกเลิก queued mutations และปฏิเสธ terminal input
  หลัง toggle โดยไม่รบกวน sessions ของ cluster อื่น
- **POL-12:** เก็บ production confirmation, GitOps ownership restrictions,
  resourceVersion guards และ RBAC เดิม; read-write ไม่ bypass guards เหล่านี้
- **POL-13:** Mode/capability ที่ไม่รู้จักต้องถูกปฏิเสธ ไม่ตกไป write mode โดยเงียบ;
  missing persisted field ของ old records เท่านั้นที่ใช้ migration default
  local code execution ที่เปิดทางให้ยิง cluster APIs เช่น user-authored synthesis
  ต้องถูกบล็อกใน read-only หรือจำกัดเป็น text-only preparation;
  การอนุญาต local editor ไม่ใช่การอนุญาตรัน arbitrary code

### UX / contracts / acceptance

Cluster settings มี “Default access mode”; header แสดง “Read-only” และเหตุผล
actions ที่ถูกบล็อกยังอธิบายได้ว่าเปิดใช้จากที่ใด; mode อยู่ใน action dialog context

`OperationPolicy`: clusterID, sessionEpoch, persistedMode, sessionReadOnly,
effectiveMode, revision. `PolicyDecision`: allowed, code, reason, revision.
Operation capability enum ต้องครอบ paths ที่ตรวจใน `service_resource.go`,
`service_authoring.go`, `service_argocd.go`, `service_stream.go` และ internal helpers

- Direct binding call ใน read-only ไม่มี Kubernetes mutation action ถูกส่ง
- Toggle ระหว่าง queued action และ dispatch บล็อกคำสั่งใหม่ได้; in-flight outcome
  แสดงตามจริง ไม่มี retry mutation หลัง reconnect โดยอัตโนมัติ
- เปลี่ยน cluster A ไม่ปิด sessions ของ B; cleanup เรียกซ้ำได้
- Mode preference round-trip ผ่าน store และ old-record migration ถูกต้อง

## 5. Explain Why — Release A

### Coverage และ rules

- **EXP-01:** รองรับ Pod, Deployment, StatefulSet, DaemonSet, Job และ PVC ก่อน;
  custom resources ใช้ condition-based findings เมื่อมีหลักฐาน ไม่เดาความหมาย operator
- **EXP-02:** Collect root object, owner-related objects, relevant container state,
  workload status/observedGeneration และ warning events; relation ตาม UID เป็นหลัก
  และตรวจ UID ของ event target ไม่รวม object เก่าที่ใช้ชื่อเดียวกัน
- **EXP-03:** MVP rules ครอบ CrashLoopBackOff, OOMKilled, ImagePullBackOff/
  ErrImagePull, scheduling failures, PVC binding failures และ rollout not ready
- **EXP-04:** แต่ละ finding มีอาการ, severity, summary, supporting evidence และ
  suggested next steps; แยก observed fact จาก possible cause เช่น OOMKilled
  ยืนยัน termination reason แต่ยังยืนยัน memory leak ไม่ได้
- **EXP-05:** ไม่ใช้สีเขียวหรือ “Healthy” จากการไม่มี evidence; ถ้าอ่าน Pods/Events
  ไม่ได้ให้ partial/unknown และอธิบายส่วนที่ขาด แม้ root GET สำเร็จ
- **EXP-06:** ไม่ fetch logs อัตโนมัติ; เสนอ Open current/previous logs เป็น user action
  ไม่แสดง mutation shortcut ที่ข้าม policy/production confirmation

### Data path และ budget

- **EXP-07:** Collector reuse resource/related readers เดิม; rule engine เป็น pure Go
  functions ที่รับ bounded snapshots และไม่ call API/AI ด้วยตัวเอง
- **EXP-08:** Cache report 5 วินาทีต่อ cluster/session/root UID; root/child update
  invalidates relevant entries; concurrent request เดียวกัน share collection
- **EXP-09:** Collection deadline 5 วินาที, concurrent API reads สูงสุด 4 ต่อ report,
  สูงสุด 100 related objects และ 100 relevant events ต่อ report; ถ้าเกิน budget
  ส่ง partial พร้อม scope/count เท่าที่รู้ ไม่สรุปว่าครบ
- **EXP-10:** Report เก็บ evidence projection ไม่เก็บ full YAML, environment values,
  Secrets หรือ logs; controller messages อาจมีข้อมูลอ่อนไหว ให้ sanitize ตาม helper เดิม
  ไม่เขียน report ลง diagnostics log/persistence อัตโนมัติ

### UX / contract

เพิ่ม tab “Explain” ใน resource inspector/detail:

1. Summary และเวลาที่ตรวจ พร้อม complete/partial/stale label
2. Findings เรียง critical → warning → informational
3. Evidence แต่ละข้อกดเปิด object/events/conditions ได้
4. Next steps ที่พาไปหน้าที่มีอยู่ เช่น Previous logs หรือ Related pods

`ExplainResource(clusterID, ref)` → `IncidentReport`:
reportID, sessionEpoch, rootRef+UID, collectedAt, coverage, findings[], issues[].
Finding: stable ruleID, severity, summary, observedFacts[], possibleCauses[],
evidenceIDs[], nextSteps[]. Evidence: sourceRef+UID, fieldPath/eventUID,
safeExcerpt, observedAt; ไม่ใส่ raw object ใน DTO

กด Refresh ทำ collection ใหม่; เปลี่ยน root/session ต้องยกเลิกหรือทิ้งผลเก่า

### Acceptance

- Deployment ที่ไม่พร้อมเพราะ child pod image pull fail แสดง root → child → reason
- OOMKilled ที่อยู่ last terminated state ยังเห็นได้เมื่อ container กลับมารันแล้ว
- Job success ไม่ถูกเรียก incident เพียงเพราะ container เคย terminated
- Events forbidden แสดง partial; object ถูกสร้างใหม่ชื่อเดิมไม่ reuse report ของ UID เก่า
- Budget timeout ให้ข้อมูลที่อ่านทันพร้อม issues; rule ไม่มีการยิง network

## 6. Query และ typed filters — Release A

### Query behavior

- **QRY-01:** รักษา text mode เดิมสำหรับ case-insensitive name substring;
  expression mode เป็นตัวเลือกชัดเจน ไม่ตีความชื่อที่มี operator เป็น expression เอง
- **QRY-02:** Expression v1 รองรับ `= != > >= < <=` และ `AND`/`&&`;
  string/status รองรับเฉพาะ equality/inequality ส่วน numeric/duration รองรับ ordering
  whitespace รอบ operator ได้; string มีช่องว่างต้อง quote
- **QRY-03:** ตัวอย่าง valid: `restarts >= 5 && age < 2h`, `cpu > 500m`,
  `memory >= 1Gi`, `status = CrashLoopBackOff`, `health != healthy`
  OR/regex/arbitrary JSONPath ไม่อยู่ใน v1; ต้องแสดง unsupported syntax
- **QRY-04:** Fields v1: name, namespace, status, health, restarts, age, cpu, memory.
  ใช้ quantity/duration types; ไม่ parse จาก formatted display strings
  unknown field, type mismatch, negative age, invalid unit หรือ syntax ต้องมี
  actionable error พร้อม position ไม่ fallback เป็น text หรือ unfiltered data
  Numeric fields ที่เป็นจำนวน/usage ไม่รับค่าติดลบ; bare CPU number คือ cores,
  bare memory number คือ bytes, age ต้องมี duration unit ส่วน restarts เป็น integer
  String equality ใน expression เป็น exact case-sensitive; name substring แบบ
  case-insensitive ยังใช้ text mode เดิม คำ `AND` ต้องเป็น uppercase หรือใช้ `&&`
- **QRY-05:** Missing numeric/metric value เป็น unknown ไม่ใช่ 0; comparisons
  รวม `!=` กับ unknown ไม่ match ใช้ `missing(cpu)` เพื่อค้น values ที่ไม่มีได้
  CPU/memory อายุเกิน 45 วินาทีถือ stale และเป็น unknown สำหรับ typed evaluation
  UI ยังโชว์ค่าล่าสุดแบบ stale ได้พร้อมเวลา
- **QRY-06:** Age คำนวณจาก server creation timestamp กับ backend clock;
  ใช้ now เดียวต่อ pass และ clamp future timestamp เป็น zero พร้อม clock warning
  refresh age query สูงสุดหนึ่งครั้ง/วินาทีเฉพาะ active view แม้ไม่มี watch event
- **QRY-07:** Metrics refresh สูงสุดทุก 15 วินาทีต่อ cluster เมื่อมี active view
  ที่แสดง/ใช้ metrics โดยไม่ขึ้นกับ pod mutation; shared fetch, timeout/backoff
  และหยุดเมื่อไม่มี consumer เพื่อไม่ปล่อย numeric filter ใช้ข้อมูลเก่าเงียบ ๆ

### Scope, cache และ selectors

- **QRY-08:** ประเมินบน complete dataset ของ requested scope ก่อน sort/window;
  หาก cold list/watch ยังไม่ครบให้ loading/partial counts ไม่ใช้คำว่า complete
- **QRY-09:** Selector inputs แยกจาก expression: Label selector และ Field selector
  ใช้ Kubernetes parser/client options; กด Apply จึงเปลี่ยน server scope
- **QRY-10:** Server list และ watch ใช้ selectors ชุดเดียวกัน รวม cold-list fallback;
  resource ไม่รองรับ field selector ให้ actionable API error ไม่เปลี่ยนเป็น empty
- **QRY-11:** Watch key และ table/cache/query identities ต้องรวม canonical selectors,
  cluster/session, GVR และ namespace; reuse unfiltered cache ได้เฉพาะเมื่อ
  implementation ยืนยันว่า evaluation เทียบเท่า ห้าม reuse narrowed cache
  เป็นคำตอบของ broader query
- **QRY-12:** Query token เปลี่ยนเมื่อ mode/expression/selectors/scope/sort เปลี่ยน;
  frontend ทิ้งผลและ delta จาก token/session เก่า ห้าม clear filter แล้วเหลือ
  match count/order/window ของ query ก่อนหน้า
- **QRY-13:** แยกจำนวน server-scope total, matched, loaded และ unknown-value rows;
  selector count ไม่อ้างว่าเป็น total ทั้ง cluster
- **QRY-14:** Compile expression ครั้งเดียวต่อ query; cap input 2 KiB,
  20 comparison/predicate terms; UI debounce text/expression 150 ms
  ไม่ยิง API ทุก keystrokeสำหรับ local terms

### Contract / acceptance

ขยาย `ListQuery` ด้วย mode, expression, labelSelector, fieldSelector;
`Filter` เดิมยังทำงานเมื่อไม่มี mode. Parse API ให้ query diagnostic ก่อน Apply
และ backend List validate ซ้ำ; ResourcePage/row events carry token/session
และ coverage. Go internal typed projection แยกจาก `ResourceRow.Fields` ที่ใช้แสดงผล

- 10k rows แต่ UI ถือ 500 rows: match ในแถวท้ายยังค้นพบ
- CPU `0.5` cores เท่ากับ `500m`, memory `1024Mi` เท่ากับ `1Gi`
- Metrics unavailable/stale ไม่ match numeric predicate แบบผิด ๆ
- Filter invalid คง last valid table พร้อม error และ label ว่ายังใช้ query เดิม
- เปลี่ยน selector/scope/reconnect ระหว่าง request ไม่ปะปน rows และไม่ leak watches

## 7. Session timeline — Release B

- **TML-01:** Timeline คือสถานะที่แอปสังเกตเห็นขณะ watch active ไม่ใช่ audit log
  ทั้ง cluster หรือประวัติก่อนเปิดแอป; แสดง observed-since ต่อ scope
- **TML-02:** Capture bounded semantic projection จาก informer add/update/delete
  ก่อน debounce ของ table; ไม่สร้าง timeline จาก final row deltas อย่างเดียว
- **TML-03:** Fields v1: health/phase, conditions (type/status/reason), replicas,
  container readiness/restarts และ termination reasons; ไม่เก็บ spec/Secret values
- **TML-04:** Initial list และ relist หลัง gap สร้าง baseline ไม่สร้าง invented
  transitions; resourceVersion/generation เปลี่ยนโดย semantic state เท่าเดิมไม่เพิ่มรายการ
  `generation` อย่างเดียวใช้กรอง status updates ไม่ได้
- **TML-05:** Key มี UID; handle deletion tombstone และ name reuse;
  observedAt ใช้เวลาของแอป ส่วน sourceTime มีเฉพาะเมื่อ object/event ให้มา
- **TML-06:** Memory limits: 500 entries/resource, 10k/cluster, 30 นาที retention,
  20 MiB projected history/cluster และ 50 MiB รวม app; เกิน bound ใด evict oldest
  พร้อม truncated count และ start time ใหม่; purge เมื่อ disconnect
- **TML-07:** Observation queue สูงสุด 1k items/cluster; saturation ไม่ block informer
  ให้ mark gap พร้อม dropped count ใน metadata ที่ไม่พึ่ง queue เดียวกัน
- **TML-08:** Watch failure/idle eviction/scope change/reconnect ต้องแสดง gaps;
  เปิด history ไม่ทำให้ watch ทุก kind หรือ bypass `maxWatches` เดิม
- **TML-09:** Event delivery เป็น bounded batch, cursor pagination ครั้งละไม่เกิน 200;
  ไม่ส่ง history ทั้งหมดทุก update

`TimelineEntry`: sequence, clusterID, sessionEpoch, GVR, namespace, name, UID,
entryType, observedAt, sourceTime?, safeChanges[].
`TimelinePage`: entries, nextCursor, retainedSince, coverage, dropped, truncated.
Expired cursor ให้ reset-required แล้ว rebase ไม่แสดง continuity ปลอม

UI: Timeline tab มี transition list, field filter และ coverage notice;
Argo Recent activity คงความหมาย Kubernetes Events เดิม แยก source ชัดเจน

Acceptance: intermediate transitions ที่รับทันก่อน coalescing ยังอยู่;
resync ไม่เพิ่มรายการซ้ำ; overload/reconnect มี gap; name reuse เริ่ม UID ใหม่;
long-running session ไม่โตเกิน bounds

## 8. Workload logs — Release B

- **LOG-01:** เปิด Logs จาก workload/service ได้เป็น group โดยใช้ ownerRef/selector
  semantics ของ existing related resolver; ไม่ใช้ label เหมารวม deployments ที่ชื่อใกล้กัน
- **LOG-02:** แต่ละ line มี pod name/UID, container, streamID, timestamp เมื่อมี
  และ received sequence; แสดงลำดับที่รับจริง ไม่อ้าง global ordering ระหว่าง pods
- **LOG-03:** Default เลือก running application containers; init/sidecars เลือกเพิ่มได้
  previous logs เป็น bounded snapshot ของ selected container instances และไม่ follow
- **LOG-04:** Follow membership ตาม UID ด้วย shared selector-aware watch;
  pod ใหม่เข้า group ได้และ pod เดิมตายปิด stream ไม่ reuse ชื่อเป็นตัวตนเดิม
- **LOG-05:** ไม่เกิน 20 concurrent streams/group, 50/app; ส่วนเกินแสดง queued
  sources และให้เลือก ไม่ซ่อนว่ากำลังดูไม่ครบ
- **LOG-06:** Backend queue/group ไม่เกิน 2k lines หรือ 4 MiB และ renderer buffer/group
  ไม่เกิน 10k lines หรือ 8 MiB; ทั้ง app logs budget 32 MiB ต่อ process side
  จำกัด line length ตาม reader เดิม 64 KiB พร้อม truncated marker
  ตัด oldest เมื่อเกินพร้อม dropped counts แทน silent loss
- **LOG-07:** Pause หยุด stream/subscription และคง bounded buffer; Resume เริ่มใหม่
  พร้อม gap marker ไม่อ้างว่าครบช่วง pause; timestamps ปิดได้แต่ source identity คงอยู่
- **LOG-08:** Source error เช่น forbidden/terminated ไม่ปิดทั้ง group;
  ไม่ retry authentication/permission errors วน; transient retry มี bounded backoff
- **LOG-09:** ปิด view/group/disconnect คืน streams/subscriptions โดย scope ถูก cluster;
  late chunks ต้องถูกทิ้งจาก session/group token

เพิ่ม `StartLogGroup`, `UpdateLogGroup`, `StopLogGroup`; single-stream API เดิมคงอยู่
`LogGroupOptions`: rootRef or explicit sources, namespace, selection,
follow/previous, tailLines. `LogGroupChunk`: groupID, sessionEpoch,
linesWithSources[], sourceStates[], dropped/truncated, coverage.

Acceptance: deployment สองตัว label ซ้ำไม่ถูก merge ผิด; replica replacement
ได้ source ใหม่; one-container denied ที่เหลือยังอ่านได้; ปิด group แล้ว stream count
กลับ baseline; pause/overload บอก gap และ memory คงอยู่ใน bounds

## 9. Saved views — Release B

- **VIEW-01:** Save/update/delete named resource query พร้อม clusterID, kind/GVR,
  namespace, queryMode/text, selectors, sort และ column IDs
- **VIEW-02:** Store ไม่เก็บ live results หรือ resource contents; query string อาจมี
  customer metadata จึงอยู่ local preference เท่านั้น ไม่ส่ง telemetry
- **VIEW-03:** เปิดจาก command palette/resource list; เปลี่ยน cluster ต้อง explicit
  และแสดง context ปลายทาง ไม่ใช้ customer name เป็น identity
- **VIEW-04:** Kind/namespace/column หายหรือ permission เปลี่ยนให้ unresolved state
  เพื่อแก้ view; ไม่ widen เป็น all namespaces และไม่ fallback cluster อื่น
- **VIEW-05:** Query format มี version; migration รักษา expression semantics
  invalid/unknown format ไม่ทำ query; จำกัด 100 views/cluster และ title 80 ตัวอักษร

Acceptance: restart แล้ว query เดิมกลับมา, สอง customer ชื่อ cluster ซ้ำไม่ชนกัน,
saved filter invalid แก้ได้โดยไม่มี API mutation

## 10. Helm inspector — Release C

- **HELM-01:** รองรับ Helm 3 Kubernetes Secret storage ใน namespace ที่เลือก;
  driver อื่นแสดง unsupported ไม่สรุปว่าไม่มี releases; unknown storage ให้บอกขอบเขต
  ที่ค้นจริง ไม่เดาว่าตรวจทุก driver แล้ว
- **HELM-02:** Release identity คือ cluster/namespace/name; revision เป็น child identity
  มี release list, history, chart/app version, status และ updated time ตามข้อมูลที่เก็บจริง
- **HELM-03:** อ่าน user-supplied values และ stored manifest ของ revision ที่เลือก;
  ไม่เรียกว่า fully computed values และไม่ทำ chart download/render/install อัตโนมัติ
- **HELM-04:** Decode ใน Go; จำกัด encoded payload 5 MiB, decompressed payload
  20 MiB/revision, timeout 5 วินาที และ 50 revisions/page; corrupted/oversized
  release ไม่ทำให้ list ที่เหลือล้ม ไม่ shell out เพื่อ decode
- **HELM-05:** Default DTO เป็น metadata กับ safe previews; raw values/manifest
  โหลดเมื่อกด Reveal สำหรับ revision นั้น และ invalidate เมื่อเปลี่ยน revision/session
  ปิด Secret data/stringData ใน default manifest; arbitrary values อาจมีความลับ
  ที่ heuristics ไม่รู้จัก จึงไม่ส่ง entire values ใน default preview
- **HELM-06:** ไม่ cache/persist raw release blob ใน app store/logs/events;
  no raw export ใน increment แรก; read-only mode อนุญาตการอ่านตาม RBAC
- **HELM-07:** RBAC forbidden แสดง forbidden; no rollback/uninstall/reconcile controls

Acceptance: same release name ต่าง namespace ไม่ชนกัน, historical revision ตรง,
malformed/decompression-limit fixtures ไม่ทำ memory spike, raw payload ไม่อยู่ใน
default binding responses หรือ event stream

## 11. Cross-cutting performance และ lifecycle

- **NFR-01:** คง virtualized table และ row deltas; ไม่ส่ง full list ทุก watch update
  parser/evidence/timeline ห้ามรัน expensive I/O บน informer callback
- **NFR-02:** Watch count cap เดิม 16/cluster และ idle eviction ต้องยังมีผลหลังเพิ่ม
  selector identities/subscriptions; UI แสดงถ้า scope หยุด live เพราะ budget
- **NFR-03:** Initial target: local filter evaluator p95 ≤ 50 ms สำหรับ 10k cached rows
  บน baseline machine; วัดแยกจาก sort, serialization, debounce และ rendering
  target query-to-visible p95 ≤ 250 ms หลัง debounce ใน warmed view เดียวกัน
  เป็น provisional targets ที่ต้องรายงานผลจริงและปรับใน spec ด้วยหลักฐานเมื่อจำเป็น
- **NFR-04:** เปรียบเทียบ baseline/candidate บนเครื่อง/dataset เดียวกัน warm-up
  อย่างน้อย 20 รอบและวัดอย่างน้อย 100 queries; recording/coverage ต้องไม่ทำให้
  unchanged-row updates เริ่มส่ง full tables
- **NFR-05:** รายงาน Go RSS และ WebView RSS แยกและรวมเท่าที่ platform วัดได้;
  active-watch/stream counts หลังปิด consumer กลับ baseline ภายใน 5 วินาที
  ยกเว้น shared informer idle cache ที่ต้องกลับตาม idle timeout เดิม
  ไม่ใช้ RSS ที่ไม่ลดทันทีเพียงอย่างเดียวสรุปว่า leak
- **NFR-06:** Network/session failure ทำข้อมูล stale, cancel owned requests และหยุด
  retry side effects; reconnect เริ่ม session epoch ใหม่ ไม่ replay actions อัตโนมัติ
- **NFR-07:** มี accessible labels, keyboard focus/escape และ readable notices ใน
  Light/Dark/System; context ต้องแสดงด้วยข้อความไม่พึ่งสี production อย่างเดียว

## 12. Test strategy และ release gates

- Pure tests: catalogue merge/version choice, parser/quantity/unknown semantics,
  deterministic incident rules, timeline dedup/retention, policy precedence
- Fake-client tests: direct bindings/helpers ไม่มี forbidden mutation,
  label/field options ตรง list/watch, object UID changes, per-source log errors
- Concurrency tests: policy toggle versus dispatch/input, query tokens,
  observation saturation, watch eviction และ scoped shutdown; ใช้ race detector
- Disposable-cluster integration: namespace-limited RBAC, partial discovery,
  list-only resource, supported/unsupported field selectors และ reconnect
- UI flows: switch cluster ระหว่าง request, query parse failure, 10k rows/windowing,
  Explain partial, timeline gap, logs pause/resume และ Helm explicit reveal
- Compatibility: old store fixtures, existing text filters, Argo actions,
  production confirmation, GitOps edit gates และ Access handoff smoke

Release A ต้องผ่าน `CRD-*`, `POL-*`, `EXP-*`, `QRY-*` และ NFR ที่เกี่ยวข้อง;
Release B เพิ่ม `TML-*`, `LOG-*`, `VIEW-*`; Release C เพิ่ม `HELM-*`
ไม่มี claims ว่าทดสอบแล้วจนมี command/result และ environment ที่ตรวจสอบได้

## 13. Source references และจุดเริ่ม implementation

Baseline implementation:

- [README architecture/behavior](../README.md)
- [Discovery](../internal/kube/discovery.go) / [CRD metadata](../internal/kube/crd.go)
  / [catalogue merge](../internal/cluster/catalogue.go)
- [Watch lifecycle](../internal/kube/informer.go) / [row cache](../internal/resources/table.go)
  / [metrics cache](../internal/resources/usage.go)
- [Pod details](../internal/resources/pod.go) / [related resources](../internal/resources/related.go)
  / [Argo activity](../internal/argocd/service.go)
- [Resource bindings](../service_resource.go) / [authoring](../service_authoring.go)
  / [Argo bindings](../service_argocd.go) / [stream bindings](../service_stream.go)
- [Log service](../internal/logs/service.go) / [session DTOs](../internal/domain/sessions.go)
  / [persistence](../internal/store/store.go)
- [Resource list](../frontend/src/views/ResourceList.vue)
  / [virtualized table](../frontend/src/components/resource/ResourceTable.vue)
  / [command palette](../frontend/src/components/common/CommandPalette.vue)

Workflow inspiration: [Sofka repository](https://github.com/nklmilojevic/sofka)
และ [official feature documentation](https://github.com/nklmilojevic/sofka/blob/main/docs/features.md)
ที่ตรวจใน feature review ก่อนเอกสารนี้ แนวคิด Explain/timeline/filters เป็น reference;
architecture, contracts, budgets และ release scope ในเอกสารนี้เป็นข้อเสนอของ Biebie Kube
