# Architecture

Koard separates workflow semantics from model and runtime integrations. The core decides what role must act and what evidence it must return; adapters decide how a specific runtime performs that request.

## Component model

Native host integration is a separate path: `nativeintegration.Setup` installs
an explicit adapter-owned asset bundle without replacing destination files.
OpenCode's `NativeBundle` supplies native agent/configuration/skill assets;
it does not invoke or weaken the proposal execution adapter. Host permissions
govern native edits, which are outside existing snapshot receipt coverage.
See [native setup](opencode-native.md) for the hybrid boundary and proof limits.

The optional `NativeEngramBundle` composes pinned, unchanged provider plugin
bytes and a local MCP entry with that same bundle. The CLI checks the installed
Engram version through its adapter before publishing; core and proposal adapters
are unchanged. Native session attribution remains owned by Engram's plugin.
Neither file installation nor dependency preflight proves host activation.

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

`internal/core/agentrouter` owns bounded, provider-neutral routing policies. `fixed` (the default) preserves the two original handoffs, planner → executor and executor → verifier. `planned` prepends orchestrator → planner, so planning actually executes. Both stop as soon as a result is not `completed`. The orchestrator is a sender-only role, never a responding model or model-selected next role.

The router validates options and every request before execution, binds each result to its request identity and destination role, and persists one workflow directory under `.koard/state/runs/<workflow_id>/`:

- `workflow.json`, the versioned outcome with every handoff and result, plus the candidate identity once the executor completes;
- `handoffs/<step>-request.json` and `handoffs/<step>-result.json`;
- `events.jsonl`, the structured event stream with monotonic sequence.

State files are written atomically, and a workflow identity is claimed with an atomic directory creation so a concurrent or replayed run fails instead of interleaving files. A claimed identity always leaves a readable record: every step writes through one object with a single persist point, so a run that stops still persists `workflow.json` with status `failed`, keeping the steps that did complete. The failure is also recorded as a diagnostic in `events.jsonl`, naming the handoff that was executing and the cause. Each hop after the first carries the previous result as a digest-bound `Context` reference, so the next role inspects the exact persisted bytes instead of a paraphrase. The router never mutates the workspace, never performs delivery operations, and never authorizes a model-returned `next` transition.

Provider execution enters through one injected runner interface. The OpenCode and Codex adapters satisfy it structurally, so the core keeps its inward-only dependency direction. `koard run` selects the policy and composes providers by destination role using `agents.planner`, `agents.executor`, and `agents.verifier`; required unresolved choices fail before run state is created. A run cancelled through its context before it starts claims no identity; one cancelled after it starts stops before the next handoff and records a `KOARD_WORKFLOW_CANCELLED` warning instead of a failure. The guided `odd run` entry point adds application-owned preparation inside the same claim; arbitrary/conditional policies, retries, and resuming an incomplete proposal workflow remain later work. See [routing.md](routing.md) for commands, evidence resolution, and authority boundaries.

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

The initial lifecycle supports `approved`, `rejected`, `correction_required`, and diagnosable `failed` outcomes. `correction_required` writes an exact request and pauses. Review consent still does not authorize correction: `DecideCorrection` records a separate immutable `granted`/`declined` decision bound to the exact review target and the exact paused correction request before `RunCorrection` can run anything.

A granted decision spends the one permitted correction: a fresh corrector receives a read-only handoff describing the findings, and its proposal is produced and captured into its own immutable snapshot by reusing the same `candidate.Produce` and `snapshot.Capture` primitives the initial workflow candidate already uses — not a parallel mechanism. The revised candidate is re-assessed for risk and independently re-reviewed as attempt 2 with its own immutable evidence. A declined decision settles the lifecycle as `rejected` without running anything. A second `correction_required` verdict escalates instead of pausing again, since `MaxCorrections` permits exactly one attempt. See `independent-review.md`.

`koard review run --workflow <id>` is the public entry point: with no review yet it runs the initial attempt, paused-with-a-decision it runs the one correction, paused-without-a-decision it refuses and names the missing step, and anything already settled it refuses outright. `koard review status` and `koard review correction status` are read-only; `koard review correction <grant|decline>` records the separate decision. The reviewer and corrector are built from the project's existing `agents.verifier` configuration — this milestone adds no new config schema. Its `provider` selects the runtime adapter (`opencode` or `codex`) through one package-level factory (`reviewProviders` in `internal/cli`), so production code builds the configured adapter while tests substitute fakes without touching the unexported adapter internals; an unrecognized provider fails closed instead of choosing one.

The instant a lifecycle settles `approved` — from either path above — it issues one immutable receipt: a content-addressed record (no cryptographic signature; the same digest-binding pattern as every other artifact here) of the exact target, reviewer, attempt, and correction budget spent. `ReceiptStatus` re-verifies it against the currently stored lifecycle and the exact snapshot it names, failing closed on any tamper or mismatch. For attempt 1 it also re-resolves the workflow's current candidate through `consent.Status` — reusing the same fresh-from-disk check `RunInitial` itself used — and rejects the candidate as stale if `workflow.json` (which, unlike every other artifact here, is not write-once) has drifted since approval. `koard review status`/`run` print the receipt as one `receipt: <id>` line; there is no dedicated receipt command. See `receipt.md`.

## Controlled mutation

`internal/core/apply` is the first capability that ever writes to the real workspace. It requires nothing beyond a verified, non-stale, approved receipt — the authority already granted through consent, review, and any correction decision. It is not mediated through the handoff/capability system at all: the content to write is already the exact, already-reviewed snapshot, so there is nothing left to negotiate a capability for (`mutation.CapabilityWorkspaceWrite` stays reserved for a different, later concern).

Every entry is read, its path validated (`snapshot.CanonicalRelativePath`, reused rather than re-derived), and its final location re-checked for containment (symlinks in the target's parent directory are resolved and must still stay inside the workspace, the same guarantee `readArtifact` already gives reads) before anything is written, then written atomically and durably — its directory entry's rename is itself synced, and an existing file's permissions are preserved rather than forced to one fixed mode. Idempotency and recovery from any interruption fall out of two properties rather than a write-ahead journal: the bytes to write are deterministic from the already-verified snapshot, and each write is atomic — so calling `Apply` again after any interruption simply re-verifies and re-writes everything, converging on the identical correct state every time. A mutable completion marker (`apply.json`, the same reasoning `workflow.json` already uses) makes a *repeat* call on an already-applied workflow a fast no-op. Apply never invokes git in any form; delivery (commit, push, pull request, merge, release) remains an entirely separate, human-driven decision. `koard review apply --workflow <id>` is its only entry point; `koard review status`/`run` print one `apply: applied` line once it has happened. See `controlled-mutation.md`.

## Project-scoped memory

`internal/core/memory` assesses optional verifier proposals and owns exact decisions
and durable at-most-one write attempts. The Engram adapter owns authenticated local
process startup, canonical project/session registration, scoped full reads, and
non-replacing saves. The CLI composes both; neither models nor recalled text choose
storage scope or grant permission. See [memory.md](memory.md).

The router accepts optional application context and verifier instructions, persisting
the exact request sent to each role. CLI evidence resolution reuses snapshot confinement
and digest checking, rejects recall file identities as source evidence, and reverses
provider path translation before handoff validation. Memory failures warn separately
without changing a successfully completed workflow's status. The guided ODD entry
point reuses this composition.

## Guided ODD application

`internal/application/oddworkflow` composes router, ODD machine, consent, review and
apply contracts without importing adapters. `odd run` persists technical preparation
inside the router's exclusive claim, before invoking any provider. `odd status`
derives progress from existing evidence without writing state or granting authority.
ODD remains open through explicit consent, review and correction, and completes only
for a registered apply matching the verified receipt. A corrected receipt uses its
own target. Legacy `run` stays unchanged. See [odd-runtime.md](odd-runtime.md) for
phase scope, read-only status and failure behavior.

## Runtime adapters

`internal/adapters/claude` implements workflow and review contracts with no model
tools. Explicit handoff references (and one hop of typed prior-result evidence)
are read through the shared snapshot confinement helper. Independent review
packages verified snapshot text, not live files. Only the bounded serialized
bundle reaches stdin; the process runs in an empty directory. Native claude.ai
login metadata is checked without reading credentials. Tool/settings restrictions
are not an OS sandbox; managed policy remains a trusted runtime dependency. See
[Claude adapter](claude-adapter.md). The workflow/review factories and ODD preflight
accept `claude` without changing core contracts or projection/sync targets.
See [the adapter guide](adapters.md) for ownership boundaries, debugging by stage,
and the checklist for adding a runtime without coupling the core.

OpenCode's process environment is derived after setting its working directory,
so inherited `PWD` cannot redirect inspection to the parent process's directory
on Unix. Proposal runs use the configured workspace; reviews use the disposable
materialized snapshot. This matters because OpenCode 2.2.6 prioritizes `PWD`
over its process cwd. Other environment values and the inspection policy remain
unchanged; this does not provide general instruction or configuration isolation.

`internal/adapters/opencode` contains the first runtime adapter. It translates one validated handoff request into an archived standalone OpenCode run and accepts only one strict JSON result bound to that request's identity and destination role. The same adapter also implements `review.Reviewer`: before a review or correction invocation, `snapshot.Materialize` reconstructs the exact reviewed snapshot as an ordinary file tree in a disposable temporary directory (verifying every entry's digest as it writes), so the adapter's read-only session inspects precisely those bytes — never the live, possibly-drifted workspace — and the directory is removed as soon as the invocation returns.

This adapter is proposal-only. It requires OpenCode 2.2.3 or newer, disables snapshots, denies every tool by default, permits only repository `read`, `glob`, and `grep`, and disables MCP servers, skills, plugins, and web search for the run. It never enables auto-approval, worktrees, or session continuation. The adapter does not route roles, persist workflow state, produce candidate identities, mutate the workspace, or authorize delivery.

Proposal and review both request `--format json`: OpenCode emits NDJSON events,
not the model result directly. The adapter binds events to one session, collects
text suffixes by assistant message, and selects the completed final step by its
start timestamp, including late reconciliation. Ambiguous ordering, incomplete
steps, transport errors and non-stop final reasons are refused. Tool/reasoning
content is never a result. The selected text still passes the strict domain
decoder: no fence stripping, JSON repair, fallback to earlier answers or retry.
Prompts describe the result fields and binding rules explicitly; they do not
assume the model can resolve a schema URI. Explicit agent selection is preserved.
Global/project instructions can still affect responses; this is not instruction
isolation. Invalid final prose remains an error, and raw model output is not
included in transport diagnostics. The event format is grounded in OpenCode
[v2.2.6 source](https://github.com/anomalyco/opencode/blob/v2.2.6/packages/cli/src/run/noninteractive.ts).

The capability guard travels as inline OpenCode configuration. OpenCode evaluates managed and Console policy after that inline configuration, so an administrator or connected workspace allow can widen the guard. Because the adapter never passes auto-approval, any permission that survives the guard still fails instead of being granted in the non-interactive run.

`internal/adapters/codex` is the second runtime adapter, satisfying the same two contracts — `agentrouter.Runner` and `review.Reviewer` — with a different mechanism. Codex has no per-tool allow-list, so the guard is its OS-level read-only sandbox (`-s read-only`) instead of a policy document: a write attempt fails inside the sandbox rather than becoming an approval decision. Every invocation also passes `--ephemeral` (no resumable session), `--ignore-user-config` and `--ignore-rules` (ambient user configuration cannot widen the guard), `-C <dir>`, `-m <model>`, and `--output-last-message <file>`. The result is read from that file because Codex interleaves progress with the answer on stdout; the read is strict and an absent or blank file is an error. Anything older than `0.149.0` is refused, since that is the only flag surface verified here. Review runs against the same materialized snapshot directory the OpenCode adapter uses. A project-level `.codex/` configuration is still read, so the adapter claims sandboxed workspace immutability, not a configuration-free run; see `codex-adapter.md`.

## Skill discovery boundary

`internal/skillregistry` provides read-only filesystem discovery and structural
validation. The CLI supplies the project directory and an explicit opt-in home
directory; ordered roots yield source paths and descriptions, not executable
authority. This package imports no runtime adapters or workflow machinery and
does not alter the configuration or handoff schemas. Project precedence and
diagnostics are deterministic; see [skills](skills.md) for the public contract.

The two project-authored skills describe planning and verification methods.
`internal/application/skillselection` freezes winning source bytes within a
claimed workflow and uses each role's runner to select allowlisted IDs/digests.
An optional router callback appends only Objective text before request
persistence. `run`/`odd run` compose this by default; empty catalogs and explicit
off preserve prior execution. Review/correction factories remain separate.
Audit sidecars record provenance, not authority; see [skills](skills.md) for
costs, bounds and proof limits. Permissions, consent, result validation and
transitions stay in the executable harness.

## Project layout

`internal/application/skillsync` composes complete-list preflight and ordered
application through the existing projection service. The optional `sync.skills`
configuration is explicit and independent of role-provider selection. CLI
composition resolves adapters; the application neither rewrites configuration
nor changes permissions. Partial application retains successful links for safe
retry; removal and rollback are not implicit. See [sync](sync.md).

`internal/application/skillprojection` exposes existing project skills through
relative directory links. A provider-neutral `Target` interface lets the runtime
adapters supply native discovery paths. CLI composition selects OpenCode or
Codex; the service reuses the registry, refuses conflicts and never changes
runtime configuration or workflow permissions. See [native projection](skills/project.md).

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
