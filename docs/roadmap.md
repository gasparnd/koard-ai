# Roadmap

Koard grows through executable vertical slices. Planned functionality is documented separately from current functionality so users and agents do not confuse intent with evidence.

## Bootstrap — in progress

- [x] Go module and CLI entry point.
- [x] `koard init` project structure generation.
- [x] `koard doctor` configuration presence check.
- [x] Human and agent documentation entry points.
- [x] Initial self-hosting configuration.
- [x] Run the Go test suite and vet checks with Go 1.27.4.

## Foundation

- [x] Versioned configuration parser and validator for schema version 1.
- [x] Provider-independent, versioned request and result contracts for planner, executor, and verifier handoffs.
- [x] Versioned ODD workflow state machine with blocking, resumption, and terminal states.
- [x] Versioned JSON Lines contract for structured events and diagnostics.

## Governed orchestration

Koard built one real but non-mutating orchestration path first, then governed the concrete candidate it produces through receipt-driven development, before finally enabling the one workspace-write path that exists: applying an exact, receipted candidate. Delivery (commit, push, pull request, merge, release) remains outside this authority entirely — orchestration expansion, below, is what remains.

### 1. Non-mutating walking skeleton

- [x] OpenCode proposal-only runtime adapter.
- [x] Agent Router v1 with a fixed planner → executor → verifier route.
- [x] Persist typed handoffs and structured events for one workflow.
- [x] Produce a candidate proposal with a deterministic identity.
- [x] Enforce a hard stop before workspace mutation, commit, push, pull request, merge, or release.
- [x] Verify that a failed or cancelled run leaves a terminal workflow record.

Replay, idempotency, and recovery after the Koard process dies move to controlled mutation, because read-only runs reconcile nothing and a retry uses a new workflow identity. Stale-candidate rejection moves to receipt-driven development, because the candidate identity binds declared references, not the bytes an immutable snapshot will capture.

### 2. Receipt-driven development

- [x] User-owned `enable`, `disable`, and `status` review mode.
- [x] Immutable candidate snapshot and identity binding.
- [x] Candidate risk assessment with passive, medium, and high tiers.
- [x] Explicit consent bound to the exact candidate snapshot.
- [x] Independent review with a bounded correction lifecycle.
- [x] Verifiable receipt tied to the reviewed candidate.
- [x] Reject a stale candidate whose snapshot no longer matches the reviewed one.
- [x] Verify snapshot integrity, reviewer independence, consent binding, and correction limits.

### 3. Controlled mutation

- [x] Apply only the exact candidate snapshot that completed the required RDD path.
- [x] Handle partial writes, interruption, and recovery deterministically.
- [x] Keep commit, push, pull request, merge, and release outside receipt authority.
- [x] Verify replay, idempotency, and recovery when the Koard process dies mid-run.

### 4. Orchestration expansion

- [x] Codex runtime adapter.
- [x] Generalized Agent Router policies beyond the fixed route (`fixed` and `planned`, exposed through `koard run`).
- [x] Engram memory adapter with scoped automatic recall, selective retention, and exact pending decisions.
- [x] Guided ODD runtime through explicit consent, review/correction and registered apply; read-only progress projection.
- [x] End-to-end multi-runtime workflow with persisted handoffs and receipts.

Validated on an isolated two-file fixture with OpenCode/GPT-OSS → Codex/Terra →
OpenCode/GPT-OSS, independent Terra review, a verified receipt, and explicitly
authorized apply. The snapshot preserved existing fixture bytes: this proves
cross-runtime orchestration and governed application, not generated source changes.
See [the validation log](../odd/tasks/opencode-output.md) for evidence and limits.

## Portable skills

- [x] Skill registry and validation (`koard skill list` / `validate`), with
  compatible discovery and optional strict Koard authoring checks. The initial
  planning and verification skills have [onboarding guides](skills.md).
- [ ] `koard skill create` generator.
- [ ] Runtime-specific skill projection.
- [ ] `koard sync` for generated settings.

## Extended portability

- [ ] Claude adapter.
- [ ] Copilot adapter.
- [ ] Capability negotiation and model selection policies.
- [ ] Migration and compatibility tooling.
