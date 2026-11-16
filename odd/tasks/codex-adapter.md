# Codex runtime adapter

Objective: add `internal/adapters/codex` as a second runtime adapter with full parity to the OpenCode adapter (proposal path plus reviewer/corrector), selected through the existing `agents.*.provider` configuration.

Branch: `task/3.1-codex-adapter` (stacked on `task/2.6-receipt`).
Delivery: `ask-on-risk`. Runner: `go test ./...`, `go vet ./...`.

## Specs

- **S1 — Package and contracts.** New package `internal/adapters/codex` exposes `Adapter`, `Config{Executable, Workspace, Model}`, `New(Config) (*Adapter, error)` and `newAdapter(Config, commandRunner)` for tests. `Adapter` satisfies `agentrouter.Runner` (`Execute`) and `review.Reviewer` (`Review`) structurally. The core never imports this package.
- **S2 — Read-only guard.** Every invocation passes exactly this surface: `exec`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`, `-s read-only`, `-C <dir>`, `-m <model>`, `--color never`, `--output-last-message <file>`. It never passes `--dangerously-bypass-approvals-and-sandbox`, `--approve-for-me`, `--add-dir`, `workspace-write`, `danger-full-access`, `-i/--image`, `resume`, `fork`, or any session continuation.
- **S3 — Version gate.** `codex --version` must parse and be at least `0.149.0`; older or unparsable versions are refused before any prompt is sent, with a message naming `0.149.0 or newer`.
- **S4 — Result capture.** The result is read from the `--output-last-message` file, never parsed from human-readable stdout. Decoding is strict: unknown fields rejected, no trailing JSON value, and an absent or empty file is an error.
- **S5 — Capability refusal.** Both paths call `mutation.RequireReadOnly` before launching any process; an unsupported capability surfaces as `*mutation.BlockedError` with zero commands run. Proposal validation also keeps the OpenCode rule: `expected_output.format` must be `application/json`.
- **S6 — Review isolation.** `Review` materializes the exact reviewed snapshot with `snapshot.Materialize` into a disposable directory, runs there (never the live workspace), and removes the directory after the invocation.
- **S7 — Provider selection.** `reviewProviders` in `internal/cli` builds the adapter from `agents.verifier.provider`: `opencode` and `codex` are supported, and any other value (including the current `unset`) fails closed naming both supported providers. `ReviewerIdentity.Runtime` reports the selected provider.
- **S8 — Tests.** Table-driven tests with a fake `commandRunner` cover: invocation surface, forbidden flags, version gate, proposal/review decode failures, capability refusal, runner failure, materialization and cleanup, and CLI provider dispatch for `codex`, `opencode`, and an unsupported value. `go test ./...` and `go vet ./...` pass.
- **S9 — Documentation.** New `docs/codex-adapter.md` explains the layer, the guard, its limits (a project-level `.codex/config.toml` is still read; the OS sandbox — not a per-tool policy — is the guard), failure modes, and why. `docs/architecture.md`, `docs/independent-review.md`, `docs/roadmap.md`, and `README.md` stay synchronized with observable behavior.
- **S10 — Evidence.** Each work unit commits with tests and docs together; the risk line names any high-risk item and native review runs when the assessment reports `review_due`.

## Tasks

| ID | Specs | Route | Status |
|---|---|---|---|
| T1 | S1-S5 | inline | done — `internal/adapters/codex/adapter.go`, `adapter_test.go` |
| T2 | S6, S8 | inline | done — materialization, `guards_test.go` |
| T3 | S7, S8 | inline | done — `internal/cli/review_run.go`, `review_providers_test.go`, `review_providers_guards_test.go` |
| T4 | S9 | inline | done — `docs/codex-adapter.md` plus architecture, review, handoff, configuration, roadmap, README |
| T5 | S10 | inline | done — commits below; independent verifier report recorded in L8 |

Risk: item 2 (security: this adapter *is* a guard) and item 5 (external CLI contract, gated by the version check).

## Log

- **L1 (2026-11-16, verbatim):** "Recupera de engram el contexto y vamos con el siguiente paso de nuestro roudmap"
- **L2 (2026-11-16, verbatim answer to the scope question):** "Paridad completa con OpenCode (Recomendado)" — the adapter implements proposal and reviewer/corrector paths plus provider selection, not a proposal-only slice.
- **L3:** Recovered state from Engram: "Controlled mutation" closed the whole "Governed orchestration" section; branch `task/2.6-receipt` at `21485e5`, clean tree. Next roadmap item is "Orchestration expansion" → "Codex runtime adapter".
- **L4:** Environment evidence: `codex-cli 0.149.0` installed, logged in with ChatGPT; `codex exec` exposes `-s read-only`, `--ephemeral`, `-C`, `-m`, `-o/--output-last-message`, `--json`, `--output-schema`, `--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`.
- **L5:** `internal/config` validates only that `agents.*.provider` is non-empty, so selecting `codex` needs no schema change. The only production consumer of an adapter today is `reviewProviders`; the router still has no CLI wiring, which stays out of scope.
- **L6 — real-run evidence (2026-11-16):** One `codex exec` run in a scratch directory with the adapter's exact surface (minus `-m`, to avoid inventing a model name): Codex reported `approval: never` and `sandbox: read-only`, refused a file write with *"patch rejected: writing is blocked by read-only sandbox"*, wrote `{"write":"blocked"}` to the `--output-last-message` file while printing progress to stdout, and stayed authenticated with `--ignore-user-config` set. This confirms the flag surface, the sandbox claim, and the file-based capture design.
- **L7 — checks (2026-11-16):** `go vet ./...` clean; `go test ./...` green across 13 packages; `go run ./cmd/koard review run` in this repository now fails closed with `agents.verifier.provider "unset" is unsupported; supported providers are "opencode" and "codex"`.
- **L8 — independent verifier (item 2):** No code blocker. Probes confirmed exact argv, model cannot add arguments, version-gate boundaries, cleanup on the failure path, stdout-only results rejected, unknown provider yields no adapter. Three documentation findings were accepted: drop "OpenCode by default" from `independent-review.md`, narrow the project-configuration limitation to tool execution rather than every descendant process, and ground the authentication claim in the observed run. Its probe suite was folded in as `guards_test.go` and `review_providers_guards_test.go`. Pre-existing `README.md` staleness about "receipts still planned" is out of scope for this unit.
- **L9 — user-visible behavior change:** a project whose `agents.verifier.provider` is `unset` (the `koard init` default) can no longer run `koard review run`; it gets the actionable error from L7 instead of an adapter built with the literal model name `unset`. This is S7's intent, and it is recorded in `docs/configuration.md` and `docs/independent-review.md`.
