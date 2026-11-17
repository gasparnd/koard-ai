# Automatic memory: core retention policy

Koard now has a provider-neutral retention coordinator. **It is not yet wired
to the CLI or a live Engram adapter.** The roadmap item remains incomplete.

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
not a deterministic privacy guarantee. The future runtime prompt must instruct
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

The planned Engram adapter must omit topic-key upserts and prompt capture, disable
its owned process's cloud autosync, and validate startup/session ownership. Shared
storage can still be synced by another client; this coordinator does not control
that client. No shared store is opened by the core tests.

## Verification and remaining work

Core tests use a fake store and temporary journals, including project isolation,
stale decisions, duplicate detection, concurrent claims and lost acknowledgements.
Run `go test ./internal/core/memory` or `go test -race ./internal/core/memory`.

The Engram transport, constrained evidence reader, runtime prompt, automatic recall,
public pending-decision commands and end-to-end checks remain tracked in
`odd/tasks/engram-memory.md`.
