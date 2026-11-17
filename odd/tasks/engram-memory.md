# Automatic project-scoped memory

Objective: integrate shared Engram with automatic selective memory and exact user decisions for substantial uncertainty. Baseline: `31357b0`; tracker: `task/4.1-engram-memory`.
Delivery: feature-branch-chain (reuse the selected chain strategy), local only; forecast about 1,500 authored changed lines. Route: inline, dependent units with no writer trigger. Checks: focused Go tests, `go test ./...`, `go vet ./...`, isolated Engram integration.

## Specs

1. S1 — "Memoria automatica y si tiene dudas sustanciales que solicite permisos o pregunte al usuario si desea guardar." Automatically select durable knowledge, permit no worthy memory, and hold uncertain proposals for an exact user decision before writing to Engram. No automatic overwrite or blind retry.
2. S2 — "comparte pero con separacion por proyecto". Reuse shared Engram storage, resolve the project's identity through Engram, scope every query/write, and check ownership of every returned record. Tests never open the real store.
3. S3 — "Con respecto alsiguite paso integremos engram". Wire automatic recall and selective persistence into the existing CLI run path, preserve provider-neutral core and read-only model permissions, and keep workflow evidence authoritative when memory is unavailable.
4. S4 — "Recuerda esto que comentas No hay una tarea explícita ni una fecha para conectarlos en el roadmap. Es un hueco de planificación que debí señalar antes. La integración encaja antes de completar el workflow de extremo a extremo, pero eso es una recomendación, no algo ya implementado.  para que lo retomemos cunado amerite." ODD runtime integration remains a remembered future planning gap, not part of this adapter slice.
5. S5 — "Esto debe poder ser intalado desde hombrew." Remember future Homebrew delivery; no installer, publishing, cloud sync or remote access in this slice.

## Tasks

- [ ] T1 — S1-S3 — inline — in progress — neutral memory proposals, local decision/write journal, strict ownership and no-overwrite policy, tests. Risk: items 2, 3, 4 (memory scope, handoff extension, concurrent decisions).
- [ ] T2 — S2-S3 — inline — pending — Engram 3.7.2 typed HTTP adapter with owned local process, explicit project/session identity, isolated transport/storage tests. Risk: items 2, 5 (process boundary and storage startup).
- [ ] T3 — S1-S5 — inline — pending — CLI automatic recall/persistence, pending-decision commands, public tests and documentation. Risk: items 2, 3 (provider composition and shared CLI contracts).
- [ ] T4 — S1-S5 — inline plus independent verifier — pending — complete checks, differential compatibility, bounded native reviews and local work-unit commits.

## Log

- L1 (2026-11-16): User: "Memoria automatica y si tiene dudas sustanciales que solicite permisos o pregunte al usuario si desea guardar. Me gustaria que se utilice el ODD y engram como lo hace gentle-ai".
- L2: User deferred ODD runtime wiring and authorized Engram now; exact follow-up is S3-S4. User later added S5 as a future note, not current implementation.
- L3: Asked shared existing Engram versus isolated storage; user: "comparte pero con separacion por proyecto".
- L4: Tagged Engram v3.7.2 source confirms typed session-owned HTTP; MCP full observation content/owner are embedded in human text, unsuitable for this typed adapter. Own a local loopback child, fresh token, strict readiness/health/active-compatibility binding; no ambient endpoint or cloud environment. Startup opens storage, so tests use explicit temporary data directories only.
- L5: Engram's `/project/current?cwd=...` supplies the canonical scope; `.koard` project display name must not substitute for it. The current display name is `koard`, while existing Engram identity is `koard-ai`.
- L6: Implementation design: verifier results may carry optional typed memory proposals backed by the handoff context. Qualified new knowledge can be saved automatically; uncertainty or differing related knowledge asks before adding a new observation, never silently replaces prior knowledge. Sensitive/unverified proposals are not persisted to Engram. Memory is advisory, never authority. Local decision identities bind exact proposal/evidence/project. Unknown write outcomes are retained, not retried automatically.
- L7: Resumed with read-only mapping and tagged-source checks. Engram POST with a topic key can overwrite content; omit topic keys and PATCH. Content deduplication may update counters, so the guarantee is no knowledge replacement, not insert-only storage. Set ENGRAM_CLOUD_AUTOSYNC=0 explicitly; session-owned mode alone does not disable cloud sync. Native startup may normalize metadata. Tests never open the real store.
- L8: First bounded T1 delivery is the optional handoff proposal contract only, with no storage or runtime prompt changes. RED observed: public decoding rejects memory_proposals; legacy-result tests pass. Proposal fields carry untrusted assessments, never save authority. Remaining T1 coordination/journaling and T2-T3 stay pending.
- L9: GREEN: go test ./internal/core/handoff -count=1, go test ./..., and go vet ./... passed. Independent verifier found no blockers for the contract unit; own boundary probes passed, 27 legacy protocol observations and six CLI forms matched baseline 31357b0 (stdout/stderr/exit). Parent added verifier boundary tests and reran TestMemoryProposalExactLimits successfully. Scratch cleaned after adjusting read-only copied module directory permissions. No real Engram store or model invoked. Risk: item 3 (shared handoff validation). Runtime harness: N/A for this protocol-only unit; functional CLI integration remains T3. Rollback: handoff memory fields/validation/tests and the new documentation section, without changing old result fields.
- L10: Local chain starts with the tracker document on task/4.1-engram-memory; first behavior slice is task/4.1-memory-contract, based on that tracker. No push or PR. T1 remains in progress after the protocol prerequisite; coordinator and journal are not implemented.
