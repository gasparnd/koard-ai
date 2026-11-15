# OpenCode proposal-only adapter

Objective: implement the first governed-orchestration milestone without enabling workspace mutation. Branch: `feature/walking-skeleton`. Delivery: `ask-on-risk`; forecast: about 300 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — User request: "Recuperá el contexto de Engram para koard-ai y continuá con el próximo paso de docs/roadmap.md."
2. S2 — Roadmap outcome: "OpenCode proposal-only runtime adapter."
3. S3 — The adapter is provider-specific and depends inward on the existing provider-neutral handoff contracts.
4. S4 — The adapter validates requests before invocation, executes OpenCode in proposal-only mode, strictly decodes one result, and validates that result against the exact handoff identity and destination role.
5. S5 — OpenCode execution must fail closed: no shell, edit, write, patch, external-directory, subagent, MCP, skill, plugin, or web-search capability; no `--auto`, worktree, or session continuation; cancellation and unsupported OpenCode versions must fail safely.
6. S6 — Agent Router, workflow persistence, structured-event persistence, deterministic candidate identity, retries/replay, CLI wiring, workspace application, and all Git or release delivery operations remain out of scope.
7. S7 — Add public package tests for observable adapter behavior, then run `go test ./internal/adapters/opencode`, `go test ./...`, and `go vet ./...`; synchronize README, architecture, handoff, and roadmap documentation with verified behavior.

## Tasks

- [x] T1 — S1-S7 — route: inline (no writer trigger; one coherent behavior) — risk: item 2 (capability guard) and item 5 (external OpenCode runtime contract) — evidence: `go test ./internal/adapters/opencode`, `go test ./...`, `go vet ./...` all pass; commit: pending.

## Log

- L1 (2026-11-15): "Recuperá el contexto de Engram para koard-ai y continuá con el próximo paso de docs/roadmap.md."
- L2 (2026-11-15): Engram observation #38 identifies the next step as the proposal-only OpenCode adapter and preserves the governed hybrid sequence.
- L3 (2026-11-15): Repository and OpenCode V2 documentation confirm a subprocess seam using `opencode run --standalone --archived`, with policy-based capability denial and strict handoff result binding.
- L4 (2026-11-15): Engram task mirror is pending because the runtime rejected the write as `foreign-project`; the local feature document remains authoritative.
- L5 (2026-11-15): RED observed first: `go test ./internal/adapters/opencode` failed with undefined adapter symbols before `adapter.go` existed. GREEN followed the implementation in `internal/adapters/opencode/adapter.go` and `adapter_test.go`.
- L6 (2026-11-15): Parent verification: focused package test, full suite, and `go vet ./...` pass; `gofmt -l` reports no formatting drift. The independent verifier could not run because the delegation provider returned `subscription_sharing_usage_limit_exceeded`, so independent proof stays unavailable rather than passing.
- L7 (2026-11-15): Documented the residual guard limitation: managed and Console OpenCode policy outrank inline configuration, so an administrator allow can widen the deny set; the adapter still passes no auto-approval flag, so surviving permissions fail instead of being granted.
