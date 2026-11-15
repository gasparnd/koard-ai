# Candidate identity

Objective: give the proposal produced by the executor a deterministic, content-derived identity and persist it with the workflow. Branch: `feature/walking-skeleton`. Delivery: `ask-on-risk`; forecast: about 350 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Produce a candidate proposal with a deterministic identity."
2. S2 — A new provider-neutral package `internal/core/candidate` depends only on `internal/core/handoff` and adds no provider coupling.
3. S3 — The identity is derived from the proposal content only: the result summary, its artifacts, and its evidence, canonically encoded. The same content always yields the same identity, and any content change yields a different one.
4. S4 — Provenance is recorded but never hashed: the candidate carries the task identity and the handoff identity that produced it, so the same proposal produced twice has one identity.
5. S5 — Only a completed result that is bound to its request may produce a candidate. A blocked or failed result, or a result that fails request binding, is rejected.
6. S6 — The router records the candidate identity in the workflow outcome when the executor hop completes, and it is persisted atomically with the rest of the state.
7. S7 — Out of scope: immutable snapshot capture, applying a candidate, RDD, replay and idempotency, stale-candidate rejection, CLI wiring, and any workspace mutation.
8. S8 — Add package tests for the observable behavior, then run `go test ./internal/core/candidate`, `go test ./...`, and `go vet ./...`; synchronize README, architecture, handoff, and roadmap documentation.

## Tasks

- [x] T1 — S1-S6, S8 — route: inline (no writer trigger; one coherent behavior) — risk: item 3 (a new identity contract later work will consume) — commit: `ebed9af`.

## Log

- L1 (2026-11-15): "vamos con la identidad", continuing `docs/roadmap.md` after the agent router work units closed.
- L2 (2026-11-15): Repo inspection found no existing candidate code; only references in docs and handoff fixtures.
- L3 (2026-11-15): `handoff.Result.Validate` already requires a non-empty summary, so an "empty proposal" branch would be dead code and is not implemented.
- L4 (2026-11-15): RED observed with undefined `Produce` and `IDPrefix`, then GREEN in `internal/core/candidate/candidate.go`.
- L5 (2026-11-15): Router wiring records the candidate only when the executor hop completes, so a blocked executor keeps its blocked outcome instead of turning into an error.
- L6 (2026-11-15): Verification: focused package tests, full suite with `-count=1`, `go vet ./...`, and `gofmt -l` all clean. Committed as `ebed9af`.
- L7 (2026-11-15): A stray `.gitignore` change adding `.atl/` appeared during the work unit; it was reverted to keep the commit scoped, and `.atl/` stays untracked.
