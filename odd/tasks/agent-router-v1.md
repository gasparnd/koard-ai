# Agent Router v1

Objective: route one workflow through the fixed planner → executor → verifier route, persisting typed handoffs and structured events without mutating the workspace. Branch: `feature/walking-skeleton`. Delivery: `ask-on-risk`; forecast: about 600 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Agent Router v1 with a fixed planner → executor → verifier route."
2. S2 — Roadmap outcome: "Persist typed handoffs and structured events for one workflow."
3. S3 — The router reuses the existing provider-neutral contracts: `internal/core/handoff` for requests/results, `internal/core/eventstream` for records, and no new provider coupling. Provider execution enters through one injected runner interface that the OpenCode adapter satisfies.
4. S4 — The fixed route is exactly two handoffs: planner → executor and executor → verifier. Each request is validated before the runner sees it, and each result is bound to its request identity and destination role. Every hop after the first carries the previous result as a digest-bound `Context` reference plus the responder artifacts, so the verifier inspects the exact persisted bytes instead of a paraphrase.
5. S5 — One workflow directory under the state root holds the workflow outcome, one request and one result file per handoff, and a JSON Lines event stream with monotonic sequence. State writes are atomic, and an existing workflow is never re-run.
6. S6 — Proposal-only: the router never mutates the workspace, commits, pushes, opens a pull request, merges, or releases. It never authorizes a model-returned `next` transition.
7. S7 — Out of scope: CLI wiring, ODD state-machine integration, Engram persistence, deterministic candidate identity, retry/replay, generalized routing policies, and RDD.
8. S8 — Add package tests for the observable behavior, then run `go test ./internal/core/agentrouter`, `go test ./...`, and `go vet ./...`; synchronize architecture, events, and roadmap documentation.

## Tasks

- [x] T1 — S1-S8 — route: inline (no writer trigger; one coherent behavior) — risk: item 6 (routing logic must be pinned by tests) — evidence: `go test ./internal/core/agentrouter`, `go test ./...`, `go vet ./...` all pass; commit: `f203f63`.
- [x] T2 — S4 — route: inline — risk: none (new behavior pinned by new tests before implementation) — evidence: `go test ./internal/core/agentrouter`, `go test ./...`, `go vet ./...` all pass; commit: `7a1ba47`.

## Log

- L1 (2026-11-15): "Vamos con la proxima tarea", after the proposal-only adapter work unit closed.
- L2 (2026-11-15): Engram observation #38 and `docs/roadmap.md` place Agent Router v1 and workflow persistence before candidate identity and the hard mutation stop.
- L3 (2026-11-15): Declared non-goals to keep the slice honest: no CLI, no ODD machine integration, no candidate identity. The ODD machine advances on workflow phases, not on role handoffs, and mapping them would invent semantics nobody requested.
- L4 (2026-11-15): RED observed with undefined router symbols, then GREEN in `internal/core/agentrouter/router.go` plus `router_test.go`.
- L5 (2026-11-15): The OpenCode adapter gained an `Execute` method that delegates to `Propose`, and `adapter_test.go` pins `var _ agentrouter.Runner = (*Adapter)(nil)` so the structural contract fails at build time.
- L6 (2026-11-15): Self-review caught a path-traversal hole: `WorkflowID` reached `filepath.Join` unvalidated. Added a single-path-segment guard with traversal and nested-path tests.
- L7 (2026-11-15): Workflow identity is claimed with atomic `os.Mkdir`, so a concurrent or replayed run fails instead of interleaving state files. Accumulated branch size now exceeds the 400-line planning heuristic; the delivery strategy decision stays open until a pull request is actually prepared.
- L8 (2026-11-15): Verification: focused package test, full suite, `go vet ./...`, and `gofmt -l` all clean.
- L9 (2026-11-15): Work unit committed as `f203f63`. RDD assessment over `8a6280b` returned `risk: medium`, `review_due: false`, `review_due_reason: under_budget`; the pending slice stays open and no native review started.
- L10 (2026-11-15): User asked whether risk item 6 was caused by switching to a free-tier model. It is not: item 6 describes the diff (no test would catch a regression), and the assessment cited only `executable_change` on the adapter path.
- L11 (2026-11-15): Re-reading the code for that answer exposed two real defects. First, the verifier handoff carried no prior result: `handoff.Request.Context` was never set, so step two received only an objective string that mentioned the original request. The old test pinned that weak behavior. S4 was under-specified and is now rewritten.
- L12 (2026-11-15): Second defect: the test helper `readRecords` treated every decoder error as end of stream, so a corrupt `events.jsonl` would pass. Replaced with `decodeRecords`, which stops only on `io.EOF`.
- L13 (2026-11-15): RED observed first: the context tests failed with an empty `Context`. GREEN followed with `resultReferences`, a digest-bound `Context` reference plus propagated artifacts, and `writeJSON` now returning the exact bytes it wrote so the digest cannot drift from the file.
- L14 (2026-11-15): Verification for T2: focused package test, full suite with `-count=1`, `go vet ./...`, and `gofmt -l` all clean.
