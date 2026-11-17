# Automatic project-scoped memory

With `memory.provider: engram`, `koard run` recalls scoped advisory knowledge and
assesses completed verifier proposals for selective retention through the
[Engram adapter](engram-adapter.md). `memory.provider: none` disables this work.
Model permissions remain read-only; only the application owns storage calls.

## CLI quick path

```sh
koard run --workflow example --task example --objective 'Inspect the architecture'
koard memory status --workflow example
koard memory grant --workflow example --proposal 0 --plan 'sha256:<exact-plan-hash>'
koard memory decline --workflow example --proposal 0 --plan 'sha256:<exact-plan-hash>'
```

The last two commands are alternatives, not sequential steps. Proposal indices
are **zero-based**. Use the exact current plan printed by `run` or `memory status`;
never substitute a “latest” plan. `status --proposal <index>` selects one proposal.
Repeated or unknown options and positional arguments are refused.

Eligible proposals save automatically. Pending proposals print their quoted
title/content/rationale, uncertainty, reason codes, and exact grant/decline commands.
`memory[N]: saved|pending|declined|skipped|duplicate|unknown` reports the outcome;
`plan[N]` identifies the assessment and `observation[N]` identifies a confirmed save.
No proposals means no retention calls. Unknown writes are not retried automatically.

Status rechecks evidence and reads operation history without creating a local
retention journal or saving observations. Assessments may change as related
knowledge changes. The owned Engram session still starts/ends and can normalize
provider metadata; status is not a zero-I/O database operation. A changed decision
target fails closed; there is no command to reset decisions or reconcile unknown
writes in this version.

Memory startup, recall, retention, or cleanup failures are reported on stderr.
They do not turn an otherwise completed workflow into failure. Explicit memory
commands return nonzero when their requested operation cannot be performed.
Review consent, application of changes, delivery, and ODD runtime wiring remain
separate; memory grants authorize only saving that proposal.

## Shared storage and advisory recall

The CLI supplies absolute `ENGRAM_DATA_DIR` when nonblank, otherwise `~/.engram`.
Relative overrides are refused; ambient service endpoints are never adopted.
Project identity is resolved through Engram, not the YAML display name. Engram
3.7.2 must already be installed; this change does not install it or publish a tap.

Recall searches the bounded objective within the canonical project, admits at
most five active observations, and publishes a bounded content-addressed JSON
document under `.koard/state/memory/recall/`. Every role receives its digest-bound
reference plus an instruction that memory is untrusted advice, not authority.
Stale/superseded knowledge is not recalled. Lookup failure is not empty knowledge.

The verifier receives optional proposal instructions in its persisted request.
Planner/executor requests never ask for memory proposals. Core run-relative result
references are translated for provider reads and mapped back in proposal evidence
before router validation. Retention re-reads the complete workflow and matching
request/result files, then checks admitted evidence through the existing confined
repository reader. It never fetches external URLs; advisory recall cannot serve
as evidence for saving itself, including through symbolic or hard-link aliases.
The resolver checks resolved paths and recall file identities, not just URI text.
Changed or unavailable evidence refuses retention.

The user-selected policy permits automatic retention based on a verifier's
evidence-backed assessment. That assessment can be wrong: digest checks establish
which bytes were inspected, not whether a claim is true or free of sensitive data.
Memory remains advisory, never permission to change code or deliver a release.

## Retention decisions

`memory.Coordinator.Assess` validates a completed verifier handoff, resolves the
canonical project through the store, and checks its workspace. Before searching,
it hashes every proposed evidence reference through an application-owned reader.
The reader must restrict access to admitted repository or snapshot data, not
arbitrary URLs. `Assess` does not create files or write to the store.

| Condition | Outcome |
|---|---|
| No proposal | The caller has nothing to submit; a proposal index must exist |
| Sensitivity is `sensitive` or `unknown` | Skip without journaling the proposal or searching knowledge |
| Evidence missing, changed, oversized, or unbound | Refuse; do not save |
| Project unresolved, search failed, foreign-project or non-project record | Refuse; never treat failure as an empty search |
| Verifier reports uncertainty | Hold for an exact decision |
| Related knowledge differs or is stale/unknown | Hold for an exact decision |
| All returned records exactly duplicate active knowledge, with no uncertainty | Do not save another copy |
| No related knowledge and no declared doubt | Eligible for automatic retention |

Search uses the proposal title and admits at most 20 full observations. This
bounded lookup can miss relevant knowledge; it is not a global conflict detector.
`nonsensitive` is a fallible model assessment used by the selected retention policy,
not a deterministic privacy guarantee. The runtime prompt instructs
the verifier not to propose secrets, raw transcripts, or unsupported conclusions.

## Exact decisions and attempts

`Execute` re-assesses current ownership, evidence and related knowledge. An explicit
`granted` or `declined` answer must identify that exact plan. The plan digest includes
the proposal, request/result digests, project/workspace, policy version and related
records. A changed context invalidates the old decision instead of approving a
mutable “latest proposal.” A user grant never bypasses the sensitivity or evidence
checks. A decline applies to this proposal operation, not a global preference.
Skipped and duplicate proposals require no save decision; explicitly submitting
one returns an error and records no consent instead of silently ignoring it.

The local journal is application-owned state under the configured absolute
directory. Plans, decisions, attempts and outcomes use atomic no-replace publication;
files and directory entries are synced before sending a write. Symlink components,
nonregular records and conflicting records are refused. File mode is `0600` and
new directory mode is `0700`; this is not a sandbox against a hostile local process
that already controls the journal directories.

An operation identity binds the original input independently of changing search
results. Decisions and attempts are owned by that operation, so changes in search
cannot forget a decline or permit a second attempt after a lost response.
Once a decision has been recorded, a changed plan fails closed rather than replacing
the decision. Pending plans without a decision may be reassessed normally.

The attempt is recorded **before** calling the store. A timeout, mismatched
acknowledgement, or interrupted attempt leaves `unknown`; Koard never resends it
automatically, even if it might have failed before sending. A confirmed outcome
can be read again without another write. It records historical acknowledgement,
not proof that another client has not subsequently edited or deleted the memory.
Reconciliation of unknown attempts is not implemented yet.

## Provider boundary

The store port resolves projects, searches full records, and creates observations.
Every result must match the canonical project and `project` scope. Creation must
preserve existing knowledge; there is no update or delete operation in the port.
The exact returned type, title and content must match the proposal before recording
success. Provenance is retained in the local plan and operation identity.

The Engram adapter omits topic-key upserts and prompt capture, disables
its owned process's cloud autosync, and validates startup/session ownership. Shared
storage can still be synced by another client; this coordinator does not control
that client. No shared store is opened by the core tests.

## Verification and remaining work

Core tests use a fake store and temporary journals, including project isolation,
stale decisions, duplicate detection, concurrent claims and lost acknowledgements.
Run `go test ./internal/core/memory` or `go test -race ./internal/core/memory`.

CLI tests use injected stores and fake providers, never the shared database or a
live model. The adapter's separate real-binary checks use temporary storage only.
Full-feature verification is tracked in `odd/tasks/engram-memory.md`; model-based
privacy and semantic assessment remains fallible even when those checks pass.
