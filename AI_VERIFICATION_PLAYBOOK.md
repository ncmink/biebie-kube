# Verification-First AI Engineering

> **Purpose:** A practical engineering playbook for AI coding assistants (including Cursor) to generate software that can be **checked**, not merely software that appears to work.
>
> **Core principle:** **No Trust, No AI.** Do not trust generated code or an AI's own claims of correctness without independent, reproducible evidence.
>
> **Audience:** Software engineers and AI coding agents working on backend, frontend, distributed systems, infrastructure, and critical business logic.

---

## 1. อธิบายแนวคิดแบบง่าย ๆ

แนวคิดจากการพูดถึง **Formal Verification** โดย Dr. Werner Vogels (CTO ของ Amazon) และข้อความที่ทีมแชร์กัน มีใจความสำคัญว่า:

- LLM สร้างโค้ดได้เร็ว แต่ไม่ได้รับประกันว่าโค้ดจะถูกต้องเสมอ
- การตรวจโค้ดด้วยตา หรือเห็นว่า compile ผ่าน ไม่ได้แปลว่าระบบถูกต้อง
- Unit/Integration Tests ทดสอบ **กรณีที่เราเลือก** จึงอาจพลาดกรณีซับซ้อน เช่น concurrency, retries หรือ state transitions ที่เกิดในลำดับแปลก ๆ
- **Formal Specification** คือการนิยามว่าระบบ *ต้อง* มีคุณสมบัติอะไร และ *ห้าม* ทำอะไร โดยใช้ภาษาหรือแบบจำลองที่มีความหมายชัดเจน
- **Formal Verification** คือการใช้วิธีทางคณิตศาสตร์พิสูจน์ว่าระบบ/แบบจำลองเป็นไปตามคุณสมบัติที่กำหนด **ภายใต้สมมติฐานและขอบเขตที่ใช้พิสูจน์**
- เป้าหมายของวิศวกรคือสร้าง **หลักฐานตรวจสอบความถูกต้อง** ให้ AI ใช้ในการพัฒนางาน ไม่ใช่เชื่อคำตอบของ AI โดยตรง

### ความหมายของแนวคิด 80/20

“ให้ AI ทำงาน 80% แล้วคนสร้างวิธีตรวจสอบ 20%” เป็น **อุปมาเรื่องการจัดสรรแรงและความสนใจ** ไม่ใช่สัดส่วนทางวิทยาศาสตร์หรือข้อรับประกันว่าการตรวจจะใช้เวลาแค่ 20% เสมอไป

**สิ่งที่ต้องจำ:** Verification ที่ดีไม่ใช่การเพิ่มจำนวน Test แบบสุ่ม แต่คือการนิยาม **สิ่งที่ต้องจริงเสมอ (invariants)** แล้วเลือกวิธีตรวจสอบให้เหมาะกับความเสี่ยง

---

## 2. ความแตกต่างของเครื่องมือตรวจสอบ

| ระดับ | วิธี | ตอบคำถามอะไร | ข้อจำกัด |
|---|---|---|---|
| 1 | Type Checking / Linting / Build | โค้ดผิดชนิด ผิด syntax หรือผิดกฎพื้นฐานไหม? | ไม่ได้พิสูจน์ business behavior |
| 2 | Unit Tests | สำหรับ input/กรณีที่เลือก ผลถูกไหม? | ไม่ได้ครอบคลุมทุกกรณีโดยอัตโนมัติ |
| 3 | Integration / E2E Tests | ระบบหลายส่วนทำงานร่วมกันถูกไหม? | ทดสอบได้เพียงบางสถานการณ์ และมักช้ากว่า |
| 4 | Property-Based / Fuzz Testing | เมื่อสร้าง input จำนวนมาก ระบบละเมิด invariant ไหม? | ยังเป็นการทดสอบ ไม่ใช่หลักฐานว่าทุกกรณีผ่าน |
| 5 | Model Checking | เมื่อสำรวจ state transitions ตาม model ที่กำหนด พบการละเมิด property หรือไม่? | ขึ้นกับความถูกต้องของ model และขอบเขต state ที่ตรวจ |
| 6 | Formal Proof / Program Verification | พิสูจน์ property ของโปรแกรมหรือแบบจำลองภายใต้ assumptions ได้ไหม? | อาจยาก/แพง; ไม่ครอบคลุม requirements ที่ลืมระบุ |

**Important:** Formal verification is not a synonym for "lots of tests." Passing tests is **not** a formal proof. A proof against the wrong specification is also insufficient.

### เครื่องมือที่น่ารู้จัก

- **Dafny** — เขียนโปรแกรมพร้อม contracts (preconditions, postconditions, invariants) และใช้ verifier ตรวจข้อพิสูจน์
- **TLA+ / TLC** — จำลองและตรวจ state transitions ของ concurrent/distributed systems; ดีสำหรับ retry, race condition, locks, ordering, leader election ฯลฯ
- **Lean** — proof assistant สำหรับทฤษฎีทางคณิตศาสตร์และการพิสูจน์โปรแกรม
- **Property-based testing libraries** — เช่น `fast-check` สำหรับ TypeScript ใช้ทดสอบ properties ด้วย input หลายรูปแบบ

เลือกใช้เครื่องมืออย่างมีเหตุผล **ไม่จำเป็นต้องนำ Dafny หรือ TLA+ มาใช้กับทุกฟังก์ชัน**

---

## 3. Instructions for Cursor / AI Coding Agent

**Treat this section as working instructions when implementing or modifying code in the repository.**

### Mission

Your task is not merely to produce code. Your task is to produce code that is **correct against an explicit specification** and to provide reproducible evidence supporting that claim.

### Mandatory workflow

When given a feature, bug fix, refactor, or architectural change:

1. **Inspect the repository first.** Identify languages, architecture, existing tests, validation, CI checks, persistence/transaction patterns, and conventions. Do not assume a tech stack or blindly introduce dependencies.
2. **Define intended behavior.** Restate key requirements, preconditions, postconditions, invariants, error handling, and out-of-scope behavior. For unclear critical requirements, surface the ambiguity rather than silently inventing a business rule.
3. **Do a risk review.** Specifically consider concurrency, retries, duplicates, partial failures, stale reads, ordering, invalid inputs, authorization, financial/data integrity, and rollback behavior where applicable.
4. **Build a verification plan before or alongside code.** Map each important invariant to an appropriate check (type check, test, property test, database constraint, model checking, proof, monitoring).
5. **Implement the minimum coherent change.** Follow repository conventions; avoid unrelated refactors and unnecessary abstractions.
6. **Verify independently.** Execute the relevant commands when tools are available. Do not infer that a test passed from reading its source.
7. **Report evidence and limitations.** List what ran, which properties it supports, what failed or was not run, and what remains unproven.

### Non-negotiable rules

- **Never write “100% correct”, “formally verified”, or “all edge cases covered” without adequate evidence and a stated proof scope.**
- **Never equate a successful build, linter result, or unit-test run with complete correctness.**
- **Never fabricate test runs, command output, CI status, logs, coverage, or model-checking results.**
- **Do not modify tests merely to make failing implementation pass** unless the original test contradicts a newly confirmed specification; explain such changes.
- **Prefer executable invariants and independent mechanisms** (e.g., database constraints, idempotency keys, checks) over asking an LLM to judge its own code.
- **Prioritize high-impact and high-risk properties.** Normal UI formatting does not need the same verification burden as authorization, money movement, or concurrency.
- **Identify assumptions explicitly.** If formal verification covers a model, explain how the production implementation is expected to match that model and where the gap may remain.
- **Do not introduce a formal-methods tool automatically.** Propose it first when its value and setup cost are justified by risk.

### Expected completion format

For significant code changes, answer in this structure:

```md
## Implemented
- What changed and why

## Specification & invariants
- Preconditions:
- Postconditions:
- Invariant 1:
- Invariant 2:

## Verification evidence
- [PASS / FAIL / NOT RUN] `command` — what it verifies
- [PASS / FAIL / NOT RUN] `command` — what it verifies

## Risk & unverified scenarios
- Remaining risks, missing checks, assumptions, or gaps

## Formal methods (only if useful)
- Proposed model/property:
- Tool (e.g., TLA+, Dafny, Lean):
- Verification scope and limitations:
```

If no commands were run, write **NOT RUN** and state why.

---

## 4. Example A: Money transfer / การโอนเงิน

Business function:

```ts
transfer(fromAccount, toAccount, amount)
```

A passing unit test for "balance 1000, transfer 100" is not enough.

### Suggested invariants

1. Amount must be positive and expressed precisely (integer minor units or exact decimal handling; not imprecise floating-point money math).
2. A successful transfer must not make the sender's available balance negative (unless overdraft is an explicit feature).
3. Within a completed transfer, the debit and credit must preserve the total ledger amount, ignoring explicitly modeled fees or external transfers.
4. A transfer must not be applied twice if the caller retries the same idempotency key.
5. Two concurrent transfers must not both succeed when their combined amount exceeds available funds.
6. Authorization must prevent moving money from an account the actor does not control.

### Race condition illustration

```text
Initial balance: 1,000

Request A: reads 1,000 -> wants to debit 800
Request B: reads 1,000 -> wants to debit 800

Both may incorrectly succeed if the implementation uses naive read-check-write.
```

### Verification plan

- Unit tests: positive amount, insufficient balance, zero/negative amount, authorization failures.
- Integration tests: real database transaction behavior, concurrent transfers, rollback on partial failure, duplicate requests.
- Property-based tests: generate legal/illegal amounts and operation sequences, check preserved properties.
- Database-level guardrails: correct transactional semantics, consistency constraints, unique idempotency key where appropriate.
- Consider TLA+ for a tricky multi-service transaction protocol; formalize what "success" and "atomicity" mean before modeling.

**Do not claim the system is safe merely because tests for single-threaded happy paths pass.**

---

## 5. Example B: Order processing with NestJS + NATS JetStream

This is an example for systems that consume messages with at-least-once delivery, retries, and multiple workers.

### Scenario

```text
API creates Order
   -> publishes OrderCreated event
   -> worker processes event
   -> writes result / triggers printing or external side effect
```

The message may be delivered more than once. A worker may crash after performing a side effect but before acknowledging it.

### Critical invariants to choose/confirm with the product owner

- Re-delivering the same event must not create a second **business record** for the same logical operation.
- An order that is canceled must not be silently moved back to active without an authorized new transition.
- The same event ID should not be processed concurrently into duplicate committed changes.
- A committed state transition should follow an explicitly allowed state-transition graph.
- Retrying an operation should not accidentally charge the customer twice.

### Important nuance: printing and external side effects

"Exactly-once printing" cannot be guaranteed simply by deduplicating a database write. Printing is an external effect: after a crash, the system may not know whether a printer has already printed. Define the desired policy explicitly, e.g., **at-least-once (possible duplicates)** versus **at-most-once (possible missing print)**, or design acknowledgment/reconciliation where the hardware supports it.

### Verification plan

- Test handler idempotency with duplicate message IDs.
- Test concurrent worker execution (including retries).
- Test crash/failure boundaries: before commit, after commit, before ack, after side effect.
- Assert DB unique constraints and transaction behavior where appropriate.
- Test out-of-order messages and stale event revisions.
- For complex workflows, consider TLA+ to model message states, acknowledgments, crash/recovery and legal transitions.

### Example property-oriented test cases

```text
Given the same event is delivered N times:
- committed business operation count remains 1 (when that is the specified rule)

Given a canceled order:
- a delayed 'start processing' event cannot transition it back to active

Given two workers consume duplicate deliveries concurrently:
- a database uniqueness constraint or atomic operation prevents duplicate records
```

Note: Business data deduplication does not necessarily imply deduplication of physical printer output.

---

## 6. How to decide whether Formal Verification is worth it

Use these questions:

1. Could a failure cause lost money, leaked data, security exposure, safety risk, or major outage?
2. Is the logic strongly dependent on concurrency, retries, timing, or complex state transitions?
3. Is there a precise invariant we can define (e.g., "no two owners for one exclusive lock")?
4. Would conventional tests struggle to reach the failing interleaving?
5. Can we model the problem small enough to analyze meaningfully, and validate that the model represents production behavior?

**Good formal-methods candidates:** distributed locking, leader election, event processing semantics, transaction protocols, complicated authorization rules, deployment controllers and reconciliation algorithms.

**Often not worth a formal proof:** presentation-only tweaks, straightforward CRUD forms, simple formatting helpers. Continue using ordinary tests for these.

### Suggested adoption path

1. **Start:** Write explicit invariants and add conventional tests around them.
2. **Improve:** Add property-based tests and failure/concurrency scenarios.
3. **Strengthen:** Add database constraints, state machines and observable audit trails.
4. **Targeted formal methods:** Model one critical protocol in TLA+ or verify a suitable algorithm in Dafny.
5. **Operational assurance:** Add monitors, reconciliation and incident tests. Formal proofs do not eliminate deployment mistakes or model/specification gaps.

---

## 7. Specification template (copy for each feature)

```md
# Feature: <name>

## Goal
<Why does this feature exist?>

## Preconditions
- <What must be true before execution?>

## Postconditions
- <What must be true after successful execution?>

## Invariants (must always hold)
- INV-001: <statement that must remain true>
- INV-002: <statement that must remain true>

## Allowed transitions
- <state A> -> <state B>

## Forbidden transitions
- <state C> -X-> <state A>

## Failure modes
- <timeout/retry/duplicate/crash/partial failure/authorization failure>

## Verification matrix
| Property | Evidence | Tool/command | Status |
|---|---|---|---|
| INV-001 | <test or proof> | <command> | NOT RUN |
| INV-002 | <test or proof> | <command> | NOT RUN |

## Assumptions and limitations
- <What the checks or proof do NOT establish>
```

---

## 8. Prompt to paste into Cursor Chat

```text
Use the instructions in @AI_VERIFICATION_PLAYBOOK.md for this task.

Before editing code:
1. Inspect the existing repository architecture and test setup.
2. Propose explicit preconditions, postconditions, and invariants.
3. Identify race conditions, retries, duplicates, partial failures, and security risks where relevant.
4. Write a verification plan and map the important invariants to tests or proof mechanisms.

Then implement the change, run the available relevant checks, and report actual verification evidence with PASS/FAIL/NOT RUN.

Never claim formal verification merely because unit tests pass. Recommend TLA+/Dafny only when justified by the problem's risk and complexity.
```

---

## 9. What this document does NOT mean

- It does **not** mean every AI-generated function requires a full mathematical proof.
- It does **not** mean unit tests are useless; they remain essential.
- It does **not** mean mathematical verification can repair an incorrect or incomplete specification.
- It does **not** guarantee that 20% human effort verifies 80% AI output.
- It does **not** mean testing every possible real-world input is universally feasible; state/model scopes and assumptions matter.

## Bottom line

**The engineer's job evolves from "write code" to "define correct behavior, create verification mechanisms, and insist on reproducible evidence."**

**AI generates. Engineering specifies. Independent checks verify. Humans remain accountable.**

---

## References for further reading

- AWS formal methods in production: https://cacm.acm.org/research/how-amazon-web-services-uses-formal-methods/
- Dafny: https://dafny.org/
- TLA+ resources: https://lamport.azurewebsites.net/tla/tla.html
- Lean: https://lean-lang.org/

This document adapts concepts from the CTO discussion and the accompanying Thai summary. Technical examples are illustrative design guidance, not claims that a specific existing project has been verified.
