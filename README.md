# Koard

Koard is a portable orchestration kit for AI-assisted software development. It coordinates specialized agents through explicit workflows and structured handoffs instead of coupling a project to one model or runtime.

Koard defines its own provider-neutral contracts, safety boundaries, and roadmap. Its runtime does not require another orchestration kit.

## Current status

Koard is building its first governed orchestration slice. The current CLI can initialize a project and strictly validate its versioned configuration. The core defines provider-neutral handoffs, a versioned ODD state machine, and a strict JSON Lines contract for workflow events and diagnostics. A fixed planner → executor → verifier router executes one workflow and persists its handoffs and events under `.koard/state/runs/`. A completed executor result becomes a candidate whose identity is the SHA-256 digest of the proposal content, and the bytes of its `repo://` artifacts are captured as an immutable, verifiable snapshot that is assessed into a passive, medium, or high risk tier. An internal OpenCode adapter executes each proposal-only handoff with read-only capabilities and strict result binding — a second Codex adapter does the same through its OS-level read-only sandbox — and every routed handoff is refused if it asks for any capability beyond `repository:read`. `koard review mode` lets the user turn receipt-driven development on or off per clone or globally, with off winning. `koard review consent` records an immutable grant or decline for independent review of one exact workflow candidate snapshot; it never authorizes mutation or delivery. The provider-neutral independent-review core validates that authority, claims one lifecycle, persists immutable request/result evidence, and pauses for a separate correction decision; a granted decision spends the one permitted correction — a fresh corrector proposes a revised candidate, which is captured and independently re-reviewed — while a declined one settles the lifecycle as rejected and a second `correction_required` verdict escalates instead of looping. `koard review run`, `koard review status`, and `koard review correction <status|grant|decline>` expose this lifecycle end to end, backed by a runtime reviewer and corrector — OpenCode or Codex, chosen by `agents.verifier.provider` — that inspect the exact reviewed snapshot materialized into a disposable directory, never the live workspace. The instant a lifecycle settles approved, it issues one immutable, digest-verifiable receipt — no cryptographic signature, the same content-addressing every other artifact here uses — printed as one line by `review status`/`run`. That receipt rejects its own candidate as stale if `workflow.json` (the one artifact here that is not write-once) has drifted since approval. Snapshot integrity, reviewer independence, consent binding, and the one-correction limit are each enforced by name with their own tests; see `docs/independent-review.md`'s verification matrix for exactly where.

`koard review apply --workflow <id>` is the first capability that ever writes to the real workspace: requiring nothing beyond that same approved, non-stale receipt, it writes the exact candidate's bytes atomically, with idempotency and interruption recovery that fall out of deterministic content rather than a write-ahead journal, and it never invokes git — delivery stays an entirely separate, human-driven decision. See `docs/controlled-mutation.md`. Engram memory is described below; skill generation remains planned.

## Quick start

With `memory.provider: engram`, runs use shared Engram 3.7.2 storage with canonical
project isolation, advisory recall, and selective verifier-proposed retention.
Substantial uncertainty requires an exact `koard memory grant` or `decline`;
`koard memory status --workflow <id>` inspects proposals without saving them.
Set `memory.provider: none` to disable memory. See [memory](docs/memory.md) for
storage configuration, failure handling, and the limits of model-based assessment.

`koard run --workflow <id> --task <id> --objective <text> [--policy fixed|planned]` executes a read-only route using the provider configured for each role. `planned` invokes a planner before executor and verifier; `fixed` preserves the original two handoffs. Review consent, independent review, and apply remain separate. See [routing](docs/routing.md) for setup and limits.

Requirements: Go 1.23 or newer.

`koard odd run` accepts the same options as `run` and starts a guided ODD workflow
through authorized apply. `koard odd status --workflow <id>` reads progress and
the next explicit action; it never grants consent or executes that action. ODD
completes only when application is registered for a verified receipt. See
[ODD runtime](docs/odd-runtime.md) for scope and limits.

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

`koard skill list [--global] [--json]` discovers existing project and optionally
global skills without installing or loading them. `koard skill validate` checks
compatibility; add `--strict` for Koard's authoring format. Start with the
[skill registry guide](docs/skills.md) and the documented planning/verification
skills. Automatic runtime projection and skill generation remain planned.

Start with [docs/README.md](docs/README.md). It explains the architecture, roadmap, contribution workflow, and how Koard will use itself during development.

## Design principles

- The orchestration core must not depend on a specific model provider.
- Agents communicate through explicit, inspectable handoff contracts.
- Project configuration and skills travel with the repository.
- Human authorization remains separate from agent execution.
- Documentation is part of the runtime context, not an afterthought.
