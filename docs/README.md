# Koard documentation

This documentation is the shared map for users, contributors, and AI agents. It describes both the product direction and the behavior that exists today.

## Reading paths

### I want to use Koard

1. Read the project `README.md`.
2. Follow the current CLI commands in `README.md`.
3. Check `roadmap.md` before relying on a planned capability.

### I want to contribute

1. Read `architecture.md`.
2. Follow `contributing.md`.
3. Use `self-hosting.md` to understand the repository's AI workflow.

### I am an AI agent

1. Start at `AGENTS.md` in the repository root.
2. Respect the architectural boundaries in `architecture.md`.
3. Verify current capabilities in `roadmap.md`; do not infer them from future examples.

## Document index

| Document | Purpose |
|---|---|
| `architecture.md` | Components, contracts, and dependency direction |
| `configuration.md` | Current configuration schema and validation rules |
| `consent.md` | Candidate-bound consent for independent review |
| `contributing.md` | Development workflow and checks |
| `events.md` | Structured workflow events and diagnostics |
| `independent-review.md` | Layer-by-layer guide to exact, read-only candidate review |
| `handoffs.md` | Versioned request and result contracts between roles |
| `odd.md` | ODD states, transitions, and interruption behavior |
| `receipt.md` | Immutable, digest-verifiable proof that a candidate completed review |
| `review-mode.md` | User-owned switch for receipt-driven development |
| `roadmap.md` | Current state and incremental milestones |
| `self-hosting.md` | How Koard will understand and improve itself |
| `skill-style-guide.md` | Contract for project-level skills |
