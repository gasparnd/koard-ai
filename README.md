# Koard

Koard is a portable orchestration kit for AI-assisted software development. It coordinates specialized agents through explicit workflows and structured handoffs instead of coupling a project to one model or runtime.

The project is inspired by the orchestration principles behind Gentle AI, but it is designed as an independent implementation with its own contracts and evolution.

## Current status

Koard is building its first governed orchestration slice. The current CLI can initialize a project and strictly validate its versioned configuration. The core defines provider-neutral handoffs, a versioned ODD state machine, and a strict JSON Lines contract for workflow events and diagnostics. A fixed planner → executor → verifier router executes one workflow and persists its handoffs and events under `.koard/state/runs/`. A completed executor result becomes a candidate whose identity is the SHA-256 digest of the proposal content, and the bytes of its `repo://` artifacts are captured as an immutable, verifiable snapshot. An internal OpenCode adapter executes each proposal-only handoff with read-only capabilities and strict result binding, and every routed handoff is refused if it asks for any capability beyond `repository:read`. `koard review mode` lets the user turn receipt-driven development on or off per clone or globally, with off winning; nothing consumes the mode yet. Review, consent, controlled mutation, Engram integration, and skill generation are still planned.

## Quick start

Requirements: Go 1.23 or newer.

```bash
go test ./...
go run ./cmd/koard init
go run ./cmd/koard doctor
```

`doctor` rejects unsupported schema versions, unknown fields, missing required values, and multiple YAML documents. See [docs/configuration.md](docs/configuration.md) for the current schema.

## Documentation

Start with [docs/README.md](docs/README.md). It explains the architecture, roadmap, contribution workflow, and how Koard will use itself during development.

## Design principles

- The orchestration core must not depend on a specific model provider.
- Agents communicate through explicit, inspectable handoff contracts.
- Project configuration and skills travel with the repository.
- Human authorization remains separate from agent execution.
- Documentation is part of the runtime context, not an afterthought.
