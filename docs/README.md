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
4. Read [skills](skills.md) to understand instruction discovery and contribute
   portable skills; follow the planning and verification guides linked there.

### I am an AI agent

1. Start at `AGENTS.md` in the repository root.
2. Respect the architectural boundaries in `architecture.md`.
3. Verify current capabilities in `roadmap.md`; do not infer them from future examples.

## Document index

| Document | Purpose |
|---|---|
| `architecture.md` | Components, contracts, and dependency direction |
| `adapters.md` | Runtime adapter purpose, request flow, troubleshooting, and extension checklist |
| `codex-adapter.md` | The Codex runtime adapter: sandbox guard, invocation surface, provider selection |
| `configuration.md` | Current configuration schema and validation rules |
| `sync.md` | Explicit skill integrations: configuration, preview, use cases and conflict recovery |
| `consent.md` | Candidate-bound consent for independent review |
| `contributing.md` | Development workflow and checks |
| `controlled-mutation.md` | The first real workspace-write capability: applying a receipted candidate |
| `events.md` | Structured workflow events and diagnostics |
| `independent-review.md` | Layer-by-layer guide to exact, read-only candidate review |
| `handoffs.md` | Versioned request and result contracts between roles |
| `odd.md` | ODD states, transitions, and interruption behavior |
| `receipt.md` | Immutable, digest-verifiable proof that a candidate completed review |
| `review-mode.md` | User-owned switch for receipt-driven development |
| `roadmap.md` | Current state and incremental milestones |
| `routing.md` | Read-only CLI execution, policies, role providers, and evidence |
| `self-hosting.md` | How Koard will understand and improve itself |
| `skill-style-guide.md` | Contract for project-level skills |
| `skills.md` | Discovery, precedence, compatible validation, safety and contributor walkthrough |
| `skills/planning.md` | Why bounded planning matters, its contract and evaluation examples |
| `skills/verification.md` | Evidence-based verification and its separation from independent review |
