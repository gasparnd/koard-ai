# Koard

Koard is a portable orchestration kit for AI-assisted software development. It coordinates specialized agents through explicit workflows and structured handoffs instead of coupling a project to one model or runtime.

Koard defines its own provider-neutral contracts, safety boundaries, and roadmap. Its runtime does not require another orchestration kit.

## Current status

Koard is building its first governed orchestration slice. The current CLI can initialize a project and strictly validate its versioned configuration. The core defines provider-neutral handoffs, a versioned ODD state machine, and a strict JSON Lines contract for workflow events and diagnostics. A fixed planner → executor → verifier router executes one workflow and persists its handoffs and events under `.koard/state/runs/`. A completed executor result becomes a candidate whose identity is the SHA-256 digest of the proposal content, and the bytes of its `repo://` artifacts are captured as an immutable, verifiable snapshot that is assessed into a passive, medium, or high risk tier. An internal OpenCode adapter executes each proposal-only handoff with read-only capabilities and strict result binding, and every routed handoff is refused if it asks for any capability beyond `repository:read`. `koard review mode` lets the user turn receipt-driven development on or off per clone or globally, with off winning. `koard review consent` records an immutable grant or decline for independent review of one exact workflow candidate snapshot; it never authorizes mutation or delivery. The provider-neutral independent-review core validates that authority, claims one lifecycle, persists immutable request/result evidence, and pauses for a separate correction decision; a granted decision spends the one permitted correction — a fresh corrector proposes a revised candidate, which is captured and independently re-reviewed — while a declined one settles the lifecycle as rejected and a second `correction_required` verdict escalates instead of looping. `koard review run`, `koard review status`, and `koard review correction <status|grant|decline>` expose this lifecycle end to end, backed by an OpenCode reviewer and corrector that inspect the exact reviewed snapshot materialized into a disposable directory, never the live workspace. The instant a lifecycle settles approved, it issues one immutable, digest-verifiable receipt — no cryptographic signature, the same content-addressing every other artifact here uses — printed as one line by `review status`/`run`. That receipt rejects its own candidate as stale if `workflow.json` (the one artifact here that is not write-once) has drifted since approval. Receipts, controlled mutation, Engram integration, and skill generation are still planned.

## Quick start

Requirements: Go 1.23 or newer.

```bash
go test ./...
go run ./cmd/koard init
go run ./cmd/koard doctor
go run ./cmd/koard review mode status
go run ./cmd/koard review consent status --workflow <id>
go run ./cmd/koard review status --workflow <id>
go run ./cmd/koard review run --workflow <id>
go run ./cmd/koard review correction status --workflow <id>
```

`doctor` rejects unsupported schema versions, unknown fields, missing required values, and multiple YAML documents. See [docs/configuration.md](docs/configuration.md) for the current schema and [docs/consent.md](docs/consent.md) for candidate-bound review consent.

## Documentation

Start with [docs/README.md](docs/README.md). It explains the architecture, roadmap, contribution workflow, and how Koard will use itself during development.

## Design principles

- The orchestration core must not depend on a specific model provider.
- Agents communicate through explicit, inspectable handoff contracts.
- Project configuration and skills travel with the repository.
- Human authorization remains separate from agent execution.
- Documentation is part of the runtime context, not an afterthought.
