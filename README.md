# Koard

Koard is a portable orchestration kit for AI-assisted software development. It coordinates specialized agents through explicit workflows and structured handoffs instead of coupling a project to one model or runtime.

The project is inspired by the orchestration principles behind Gentle AI, but it is designed as an independent implementation with its own contracts and evolution.

## Current status

Koard is in its bootstrap phase. The current CLI can initialize and diagnose the project-level configuration. Model adapters, Engram integration, ODD execution, and skill generation are planned but are not implemented yet.

## Quick start

Requirements: Go 1.23 or newer.

```bash
go test ./...
go run ./cmd/koard init
go run ./cmd/koard doctor
```

## Documentation

Start with [docs/README.md](docs/README.md). It explains the architecture, roadmap, contribution workflow, and how Koard will use itself during development.

## Design principles

- The orchestration core must not depend on a specific model provider.
- Agents communicate through explicit, inspectable handoff contracts.
- Project configuration and skills travel with the repository.
- Human authorization remains separate from agent execution.
- Documentation is part of the runtime context, not an afterthought.
