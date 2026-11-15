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

## Agent routing

`internal/core/agentrouter` owns the first fixed orchestration route. Version 1 composes exactly two provider-neutral handoffs, planner → executor and executor → verifier, and stops as soon as a result is not `completed`.

The router validates options and every request before execution, binds each result to its request identity and destination role, and persists one workflow directory under `.koard/state/runs/<workflow_id>/`:

- `workflow.json`, the versioned outcome with every handoff and result, plus the candidate identity once the executor completes;
- `handoffs/<step>-request.json` and `handoffs/<step>-result.json`;
- `events.jsonl`, the structured event stream with monotonic sequence.

State files are written atomically, and a workflow identity is claimed with an atomic directory creation so a concurrent or replayed run fails instead of interleaving files. A claimed identity always leaves a readable record: every step writes through one object with a single persist point, so a run that stops still persists `workflow.json` with status `failed`, keeping the steps that did complete. The failure is also recorded as a diagnostic in `events.jsonl`, naming the handoff that was executing and the cause. Each hop after the first carries the previous result as a digest-bound `Context` reference, so the next role inspects the exact persisted bytes instead of a paraphrase. The router never mutates the workspace, never performs delivery operations, and never authorizes a model-returned `next` transition.

Provider execution enters through one injected runner interface. The OpenCode adapter satisfies it structurally, so the core keeps its inward-only dependency direction. Routing policies beyond the fixed route, ODD state-machine integration, retries, resuming an incomplete workflow, cancellation, and CLI wiring remain later work.

## Candidate identity

`internal/core/candidate` turns one completed executor result into a candidate with a content-derived identity. The identity is the SHA-256 digest of a canonical proposal: the result summary, its artifacts, and its evidence.

Two consequences are deliberate. Identical content always produces the same identity, even across separate workflows. Provenance is recorded beside the identity and never hashed, so a candidate carries the task identity and the handoff that produced it without the identity depending on them.

The package depends only on `internal/core/handoff` and rejects any result that is not `completed` and bound to its request. Artifact order is preserved because it is part of the proposal, not a set.

The identity binds what the protocol actually carries: declared references and the summary, not the bytes those references point at. Immutable snapshot capture, applying a candidate, RDD, and stale-candidate rejection remain later work.

The router records the candidate in `workflow.json` as soon as the executor hop completes, and a blocked or failed executor produces no candidate at all.

## Mutation boundary

`internal/core/mutation` owns the only capability set version 1 permits: `repository:read`. Every other value is refused by name, and an unrecognized capability is refused too, so the guard fails closed instead of tolerating a spelling it did not anticipate.

The router checks the requested capabilities before it creates any state, so a refused run leaves nothing behind. The OpenCode adapter checks the same set before spawning a process, which makes one list the single source of truth for both callers instead of two comparisons that can drift.

The boundary covers what no code path performs yet. Nothing in Koard commits, pushes, opens a pull request, merges, or releases, and applying a candidate does not exist, so those capabilities are refused explicitly rather than left advertised and unimplemented.

## Runtime adapters
`internal/adapters/opencode` contains the first runtime adapter. It translates one validated handoff request into an archived standalone OpenCode run and accepts only one strict JSON result bound to that request's identity and destination role.

This adapter is proposal-only. It requires OpenCode 2.2.3 or newer, disables snapshots, denies every tool by default, permits only repository `read`, `glob`, and `grep`, and disables MCP servers, skills, plugins, and web search for the run. It never enables auto-approval, worktrees, or session continuation. The adapter does not route roles, persist workflow state, produce candidate identities, mutate the workspace, or authorize delivery.

The capability guard travels as inline OpenCode configuration. OpenCode evaluates managed and Console policy after that inline configuration, so an administrator or connected workspace allow can widen the guard. Because the adapter never passes auto-approval, any permission that survives the guard still fails instead of being granted in the non-interactive run.

## Project layout

```text
cmd/koard/        CLI executable
internal/cli/     Command routing and user-facing behavior
internal/core/    Provider-independent contracts and orchestration
internal/adapters Runtime and service integrations
.koard/           Project-level Koard configuration
skills/           Project skills and local references
docs/             Human and agent documentation
```

## Configuration boundary

`.koard/koard.yaml` selects roles, workflows, memory, and adapters. The parser accepts only the explicitly versioned schema documented in `configuration.md`; provider-specific settings must enter through future adapter contracts rather than leak into the core schema. Secrets must never be stored there. Authentication remains the responsibility of each provider's established credential mechanism.
