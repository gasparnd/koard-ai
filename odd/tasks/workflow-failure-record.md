# Workflow failure record

Objective: a claimed workflow always leaves a readable terminal record, so a run that stopped is never mistaken for one that never started. Branch: `feature/walking-skeleton`. Delivery: `ask-on-risk`; forecast: about 250 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Verify replay, idempotency, cancellation, crash recovery, and stale-candidate rejection." This work unit covers only the crash slice: the record survives a failure.
2. S2 — When the route fails for any reason after the workflow directory is claimed, `workflow.json` is persisted with status `failed`, and the steps that did complete stay in `handoffs` and `results` together with any candidate they produced.
3. S3 — The failure is recorded in `events.jsonl` as a diagnostic. A result that fails binding keeps its existing `KOARD_HANDOFF_INVALID` diagnostic; every other failure records `KOARD_WORKFLOW_FAILED`. The diagnostic detail names the handoff that was executing and the cause.
4. S4 — The original failure still reaches the caller. Persisting the record never swallows it, and when the record cannot be written both errors are reported together.
5. S5 — There is exactly one persist point. The old code persisted on two paths and abandoned state on six error returns; that split is what allowed a claimed identity to be left with no record.
6. S6 — Non-goals: reusing a `WorkflowID`, resuming or replaying an incomplete workflow, cancellation semantics, and idempotency. Runs are read-only, so nothing is reconciled and a retry uses a new identity. The record exists to prove what happened, not to resume it.
7. S7 — Add router tests for each observable above, then run `go test ./internal/core/agentrouter`, `go test ./...`, and `go vet ./...`; synchronize `docs/events.md` and `docs/architecture.md`.

## Tasks

- [x] T1 — S1-S5, S7 — route: inline (no writer trigger; one coherent behavior) — risk: none — commit: `7c65e4f`.

## Log

- L1 (2026-11-15): "Dale sigamos", accepting "¿Seguimos con crash recovery entonces?".
- L2 (2026-11-15): Inspection of `internal/core/agentrouter/router.go` found two persist points (`:148` and `:165`) and six error returns that abandon the claimed directory (`:113`, `:118`, `:121`, `:127`, `:130`, `:135`, `:138`, `:153`). The claimed identity is then poisoned: a retry dies at "already exists" with no evidence of why.
- L3 (2026-11-15): `TestRouteRejectsUnboundResult:198` pinned the defect itself, asserting that no `workflow.json` exists after a binding failure. Its intent ("must not persist a completed outcome") is right; the assertion was too blunt. It is rewritten to require a `failed` record with no results.
- L4 (2026-11-15): `eventstream.Record.Validate` refuses a record carrying both `Event` and `Diagnostic` payloads, and `Diagnostic` has no `handoff_id` field. The failing handoff is therefore named in the diagnostic detail instead of extending the event schema.
- L5 (2026-11-15): RED observed on five assertions before implementing: returned status was `""` instead of `failed`, no `workflow.json` existed after a binding failure, the last event-stream record was an event rather than a diagnostic, and the completed step was absent from the outcome. `TestRouteKeepsOneDiagnosticForAnUnboundResult` passed before the change, so it pins existing behavior instead of the new one.
- L6 (2026-11-15): GREEN by moving the run into a `routing` value with one `settle` persist point. That removed the two old persist sites and the six returns that abandoned state; no error is swallowed, and a failure to write the record joins with the failure that stopped the run.
- L7 (2026-11-15): Verification: `go test ./internal/core/agentrouter -count=1`, `go test ./... -count=1`, `go vet ./...`, and `gofmt -l internal cmd` all clean. The twelve pre-existing router tests are unchanged and pass, which is the differential evidence that the success and blocked paths behave as before.
- L8 (2026-11-15): RDD assess over `8a6280b..HEAD`: `risk: medium`, `review_due: false`, `review_due_reason: under_budget`. No native review started.
- L9 (2026-11-15): One observable did change outside the request: the `KOARD_HANDOFF_INVALID` diagnostic detail now includes the failing handoff identity, because the detail carries the wrapped error. `docs/events.md` was updated for that and for the new code.
