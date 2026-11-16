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

Provider execution enters through one injected runner interface. The OpenCode adapter satisfies it structurally, so the core keeps its inward-only dependency direction. A run cancelled through its context before it starts claims no identity; one cancelled after it starts stops before the next handoff and records a `KOARD_WORKFLOW_CANCELLED` warning instead of a failure. Routing policies beyond the fixed route, ODD state-machine integration, retries, resuming an incomplete workflow, and CLI wiring remain later work.

## Candidate identity

`internal/core/candidate` turns one completed executor result into a candidate with a content-derived identity. The identity is the SHA-256 digest of a canonical proposal: the result summary, its artifacts, and its evidence.

Two consequences are deliberate. Identical content always produces the same identity, even across separate workflows. Provenance is recorded beside the identity and never hashed, so a candidate carries the task identity and the handoff that produced it without the identity depending on them.

The package depends only on `internal/core/handoff` and rejects any result that is not `completed` and bound to its request. Artifact order is preserved because it is part of the proposal, not a set.

The identity binds what the protocol actually carries: declared references and the summary, not the bytes those references point at. The candidate snapshot binds the bytes.

The router records the candidate in `workflow.json` as soon as the executor hop completes and its snapshot is captured, and a blocked or failed executor produces no candidate at all.

## Candidate snapshot

`internal/core/snapshot` captures the exact bytes a candidate proposes. When the executor completes, the router reads every artifact from the workspace and stores it as a read-only, content-addressed blob under `<run>/snapshot/blobs/sha256/`, then writes `<run>/snapshot/manifest.json`. The manifest lists the candidate identity and, in artifact order, each artifact's kind, URI, `sha256:` digest of the captured bytes, and size. The snapshot identity is the SHA-256 digest of the manifest and is recorded as `snapshot_id` beside the candidate identity, which the snapshot does not change.

Only `repo://` artifacts can be captured. The path must be canonical and relative, must stay inside the workspace after resolving symlinks, and must name a regular file; the file opened must be the file validated. A declared digest must match the captured bytes. A single artifact may hold at most 8 MiB and a snapshot at most 32 MiB in total, which bounds the memory a capture uses. Capture observes cancellation before reading each artifact and before publishing each blob; a run cancelled during capture records `KOARD_WORKFLOW_CANCELLED` instead of a refusal. Every artifact is checked before anything is written, so any refusal fails the run with a `KOARD_SNAPSHOT_REFUSED` diagnostic, records no candidate, and leaves no manifest. Blobs and the manifest are published atomically and never overwritten.

`snapshot.Load` verifies a stored snapshot against the identity a candidate recorded: the manifest must hash to it and every blob must still match its digest. Evidence stays declared, and the verifier does not yet receive the snapshot. Candidate-bound consent now verifies this stored snapshot before recording a decision; stale-candidate rejection and applying a candidate remain later work.

## Risk assessment

`internal/core/risk` classifies a verified candidate snapshot into a `passive`, `medium`, or `high` tier using Koard's rules below. Every list is closed in the package so a candidate can never lower its own tier through repository configuration.

| Tier | Reason code | Signal |
|---|---|---|
| `passive` | `non_executable_only` | Every artifact is documentation (`.md`, `.rst`), or there are no artifacts |
| `medium` | `configuration_change` | `go.mod`, `go.sum`, `Dockerfile`, `Makefile`, `.json`, `.yaml`, `.yml`, `.toml` |
| `medium` | `executable_change` | Any other artifact, including unknown types |
| `high` | `shell_source` | `.sh`, `.bash`, `.zsh`, a leading `#!` line, or a file under `.github/workflows/` |
| `high` | `process_boundary` | A leading `#!` line, an `os/exec` import, `os.StartProcess`, or `syscall.Exec` |
| `high` | `hot_path` | A path segment starting with `auth`, `secur`, `secret`, `credential`, `permission`, or `sandbox` |
| `high` | `agent_escalation` | The caller cites high-risk item 1-6 with a reason; escalation only raises |
| `high` | `unassessable` | The snapshot cannot be loaded and verified |

The tier is the highest any artifact or escalation reaches, and the reasons list only that tier's signals, in artifact order, without duplicates. Size never changes the tier. `review_due` is true only for `high`. The router stores the assessment as `risk` in `workflow.json` beside the candidate. Candidate-bound consent and the independent-review target preserve the tier as part of their exact binding. Commit-slice budgeting is not part of the current roadmap.

## Mutation boundary

`internal/core/mutation` owns the only capability set version 1 permits: `repository:read`. Every other value is refused by name, and an unrecognized capability is refused too, so the guard fails closed instead of tolerating a spelling it did not anticipate.

The router checks the requested capabilities before it creates any state, so a refused run leaves nothing behind. The OpenCode adapter checks the same set before spawning a process, which makes one list the single source of truth for both callers instead of two comparisons that can drift.

The boundary covers what no code path performs yet. Nothing in Koard commits, pushes, opens a pull request, merges, or releases, and applying a candidate does not exist, so those capabilities are refused explicitly rather than left advertised and unimplemented.

## Review mode

`internal/core/reviewmode` owns the user's switch for receipt-driven development. Two setting files decide it: a global one in the user's configuration directory and a clone-local one in `.koard/state/`, which git ignores. Off wins over on, and with nothing set the mode is on. The committed `koard.yaml` cannot carry the mode, so a repository change can never flip it. A setting file that cannot be trusted is an error, never a silent "on". New candidate consent decisions require the effective mode to be on; status remains readable. See `review-mode.md`.

## Candidate-bound consent

`internal/core/consent` resolves one completed workflow into an exact target containing the workflow, candidate, and snapshot identities plus its risk tier. Resolution strictly decodes `workflow.json`, requires the candidate and risk assessment to bind the same snapshot, and verifies the immutable manifest and every blob before a decision can be recorded.

One versioned `consent.json` file under the workflow run directory records `granted` or `declined` with the authority `review`. Publication is atomic and never replaces an existing record. Repeating the same decision is idempotent; a conflicting decision or target mismatch fails closed. A decline affects only that candidate snapshot. A grant permits only independent review of those exact stored bytes: correction, workspace mutation, commit, push, pull request, merge, and release remain unauthorized.

The CLI resolves the target by workflow identity through `koard review consent <status|grant|decline> --workflow <id>`. `status` never writes. Grant and decline require review mode to be on. Review-mode writes and consent decisions share one global cross-process lock, so disabling review mode cannot complete between the consent check and publication. A stale lock fails closed instead of guessing that no operation is active. See `consent.md`.

## Independent review lifecycle

`internal/core/review` owns provider-neutral review state. Before claiming a lifecycle it reuses `internal/core/consent` to require a granted decision and to verify the exact workflow, candidate, snapshot, risk tier, manifest, and blobs. This ordering matters: refused authority or corrupt evidence leaves no review state and spends no provider work.

After validation and a live check that review mode is on, the core stages `attempts/` and the initial `review.json` in a sibling temporary directory and publishes both with a single atomic rename into `review/`; a reader or a racing claimant only ever sees no review directory or the complete one, never one left half-written by a crash between separate creation steps. Review mode is checked again immediately before invoking the reviewer, so a revocation during claim preparation or a slow provider start still stops the attempt. The core persists an immutable request before calling a reviewer and accepts only a strict result bound to that request's target and attempt. Reviewer identity comes from trusted application configuration and is present in the request and lifecycle, but deliberately absent from model output. The model can report findings and a verdict; it cannot choose its identity, capabilities, state transition, correction budget, or delivery authority.

The initial lifecycle supports `approved`, `rejected`, `correction_required`, and diagnosable `failed` outcomes. `correction_required` writes an exact request and pauses. Review consent still does not authorize correction: a later core slice and public command must record a separate candidate-scoped decision before any proposal-only correction can run. See `independent-review.md`.

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
