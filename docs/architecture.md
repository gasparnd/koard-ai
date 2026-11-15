# Architecture

Koard separates workflow semantics from model and runtime integrations. The core decides what role must act and what evidence it must return; adapters decide how a specific runtime performs that request.

## Component model

```text
CLI
 └── Application services
      ├── ODD workflow engine
      ├── Agent router
      ├── Handoff protocol
      ├── Memory port
      └── Skills registry
           ├── Claude adapter
           ├── Codex adapter
           ├── Copilot adapter
           ├── OpenCode adapter
           └── Engram adapter
```

## Dependency rule

Dependencies point inward. Core packages may define ports for models, memory, and storage, but they must not import provider implementations. Provider packages may import and implement core contracts.

## Agent communication

Agents do not receive authority by sending conversational messages to one another. The orchestrator owns transitions and exchanges a structured handoff containing:

- task identity and objective;
- role and allowed capabilities;
- bounded context references;
- expected output schema;
- verification evidence;
- terminal status and follow-up transition.

The first protocol version is persisted as strict JSON so that humans and different runtimes can inspect the same artifact. Requests state bounded work; results bind back to the exact handoff identity and responding role. A result may recommend a next role, but only the orchestrator may authorize the next transition. See `handoffs.md` for the current contract.

## Workflow state

The provider-neutral state machine in `internal/core/odd` owns legal ODD transitions. It is a pure value: applying an accepted event returns the next revision, while an invalid event leaves the previous snapshot unchanged. Active work can be blocked and resumed at the exact previous state; completed and stopped workflows are terminal. See `odd.md` for the transition table.

The state machine does not execute models, write event logs, or authorize delivery. Those responsibilities remain outside the transition core.

## Events and diagnostics

`internal/core/eventstream` defines a strict JSON Lines contract for observable workflow output. Every record has a version, identity, workflow identity, monotonic sequence supplied by the orchestrator, and timestamp. A record contains exactly one event or diagnostic payload.

The contract does not choose storage, generate identities, or infer event meaning from text. The future orchestrator will create records from accepted state transitions and validated handoff outcomes. See `events.md` for the wire format.

## Runtime adapters

`internal/adapters/opencode` contains the first runtime adapter. It translates one validated handoff request into an archived standalone OpenCode run and accepts only one strict JSON result bound to that request's identity and destination role.

This adapter is proposal-only. It requires OpenCode 2.2.3 or newer, disables snapshots, denies every tool by default, permits only repository `read`, `glob`, and `grep`, and disables MCP servers, skills, plugins, and web search for the run. It never enables auto-approval, worktrees, or session continuation. The adapter does not route roles, persist workflow state, create candidate identities, mutate the workspace, or authorize delivery.

The capability guard travels as inline OpenCode configuration. OpenCode evaluates managed and Console policy after that inline configuration, so an administrator or connected workspace allow can widen the guard. Because the adapter never passes auto-approval, any permission that survives the guard still fails instead of being granted in the non-interactive run.

## Project layout

```text
cmd/koard/        CLI executable
internal/cli/     Command routing and user-facing behavior
internal/core/    Future provider-independent contracts
internal/adapters Runtime and service integrations
.koard/           Project-level Koard configuration
skills/           Project skills and local references
docs/             Human and agent documentation
```

## Configuration boundary

`.koard/koard.yaml` selects roles, workflows, memory, and adapters. The parser accepts only the explicitly versioned schema documented in `configuration.md`; provider-specific settings must enter through future adapter contracts rather than leak into the core schema. Secrets must never be stored there. Authentication remains the responsibility of each provider's established credential mechanism.
