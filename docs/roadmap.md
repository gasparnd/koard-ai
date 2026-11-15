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

Koard will build one real but non-mutating orchestration path before RDD. RDD will then govern the concrete candidate produced by that path before any workspace mutation is enabled.

### 1. Non-mutating walking skeleton

- [x] OpenCode proposal-only runtime adapter.
- [x] Agent Router v1 with a fixed planner → executor → verifier route.
- [x] Persist typed handoffs and structured events for one workflow.
- [x] Produce a candidate proposal with a deterministic identity.
- [x] Enforce a hard stop before workspace mutation, commit, push, pull request, merge, or release.
- [ ] Verify replay, idempotency, cancellation, crash recovery, and stale-candidate rejection.

### 2. Receipt-driven development

- [ ] User-owned `enable`, `disable`, and `status` review mode.
- [ ] Immutable candidate snapshot and identity binding.
- [ ] Candidate risk assessment with passive, medium, and high tiers.
- [ ] Explicit consent bound to the exact candidate snapshot.
- [ ] Independent review with a bounded correction lifecycle.
- [ ] Verifiable receipt tied to the reviewed candidate.
- [ ] Verify snapshot integrity, reviewer independence, consent binding, and correction limits.

### 3. Controlled mutation

- [ ] Apply only the exact candidate snapshot that completed the required RDD path.
- [ ] Handle partial writes, interruption, and recovery deterministically.
- [ ] Keep commit, push, pull request, merge, and release outside receipt authority.

### 4. Orchestration expansion

- [ ] Codex runtime adapter.
- [ ] Generalized Agent Router policies beyond the fixed route.
- [ ] Engram memory adapter based on observed persistence requirements.
- [ ] End-to-end multi-runtime workflow with persisted handoffs and receipts.

## Portable skills

- [ ] Skill registry and validation.
- [ ] `koard skill create` generator.
- [ ] Runtime-specific skill projection.
- [ ] `koard sync` for generated settings.

## Extended portability

- [ ] Claude adapter.
- [ ] Copilot adapter.
- [ ] Capability negotiation and model selection policies.
- [ ] Migration and compatibility tooling.
