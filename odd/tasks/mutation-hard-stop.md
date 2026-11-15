# Mutation hard stop

Objective: make every mutating capability mechanically unusable before an apply path exists. Branch: `feature/walking-skeleton`. Delivery: `ask-on-risk`; forecast: about 300 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Enforce a hard stop before workspace mutation, commit, push, pull request, merge, or release."
2. S2 — A new provider-neutral package `internal/core/mutation` owns the only capability set version 1 permits, `repository:read`, and refuses every other value by name. An unknown or misspelled capability fails closed instead of being tolerated.
3. S3 — The router refuses to start a run whose requested capabilities are not read-only, and it refuses before creating or writing any state, so a blocked run leaves nothing behind.
4. S4 — The OpenCode adapter enforces the same guard instead of its own hardcoded comparison, so one list is the single source of truth and cannot drift between callers.
5. S5 — A blocked capability is a typed error that names it, so a caller can distinguish a mutation stop from an ordinary failure.
6. S6 — Out of scope: an apply path, RDD receipts, real Git or release operations, replay, cancellation, and recovery of a partial run.
7. S7 — Add package tests for the observable behavior, then run `go test ./internal/core/mutation ./internal/core/agentrouter ./internal/adapters/opencode`, `go test ./...`, and `go vet ./...`; synchronize README, architecture, and roadmap documentation.

## Tasks

- [x] T1 — S1-S5, S7 — route: inline (no writer trigger; one coherent behavior) — risk: item 2 (a security guard) — commit: `93deed3`.

## Log

- L1 (2026-11-15): "vamos con el freno de mano", continuing `docs/roadmap.md` after the candidate identity work unit closed.
- L2 (2026-11-15): Inspection found the OpenCode adapter already rejected non-read capabilities with a hardcoded comparison in `validateProposalRequest`. Leaving it there would mean two guards that can drift, so the duplicate is replaced by one shared guard.
- L3 (2026-11-15): RED observed with an undefined `Capabilities` field and an unknown `mutation` package, then GREEN in `internal/core/mutation/mutation.go`, the router guard, and the adapter call site.
- L4 (2026-11-15): Independent verification ran and returned PASS on S2-S7 with no blockers. Its strongest evidence was a defused-guard experiment: with `RequireReadOnly` forced to allow everything, the router and adapter tests failed, which proves the tests bite. Removing each call site separately failed only that call site's tests, so both are pinned.
- L5 (2026-11-15): The verifier found one real defect: `BlockedError` rendered an empty capability as nothing, so the message named no value. Fixed with quoted rendering, and pinned by a table of values designed to look like the read capability.
- L6 (2026-11-15): The verifier's probe for an untouched state root was folded into `TestRouteRejectsMutatingCapabilitiesBeforeTouchingState`, which now asserts the whole state directory is empty instead of only the workflow directory.
- L7 (2026-11-15): Verification: focused tests, full suite with `-count=1`, `go vet ./...`, and `gofmt -l` all clean. Independent verification unavailable items remain: a live end-to-end refusal through a real OpenCode subprocess, which needs an authorized invocation.
