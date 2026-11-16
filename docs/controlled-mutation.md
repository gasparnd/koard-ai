# Controlled mutation

Controlled mutation is the first capability in Koard that ever writes to the real workspace. Everything before this layer — handoffs, candidates, snapshots, review, correction, the receipt — exists specifically so that by the time any byte actually changes on disk, the path to get there was entirely read-only, auditable, and already approved.

## Why this layer exists

Every role in the walking skeleton, every reviewer, every corrector is bound to `repository:read` and nothing else. That boundary (`internal/core/mutation`) has refused every wider capability since the project's first commit, with a message that says it plainly: *"is not permitted before an apply path exists."* This is that apply path — and only this path. No handoff capability was widened to get here; nothing untrusted decides what gets written.

## Why this is not mediated through capabilities

A model proposing a change and a model being granted write access are different concerns, and this milestone deliberately keeps them separate. By the time `Apply` runs, the content to write is not a proposal anymore — it is the exact, already-reviewed, already-receipted snapshot. There is nothing left to negotiate a capability for: the bytes are fixed, and the only question left is "does the authority to write them exist?" `mutation.CapabilityWorkspaceWrite` stays reserved and unused; it describes a different, later concern — a model-driven role requesting to write — not this one.

## Authority

`Apply` requires exactly one thing: a verified, non-stale, approved `review.ReceiptStatus` for the workflow. Nothing more.

This is deliberate. By the time a receipt exists, a human has already granted review consent, review has already independently approved the exact candidate, and — if a correction happened — a human has already separately decided to grant it. Requiring yet another confirmation at apply time would be ceremony on top of authority that already exists; every other privileged action in this codebase already works this way, with authority living in a persisted artifact rather than an interactive prompt.

## Mechanism

For every entry in the receipted snapshot's manifest:

1. Read it back (`snapshot.ReadEntry`), which re-verifies its digest.
2. Validate its `repo://` URI resolves to a canonical, local, relative path (`snapshot.CanonicalRelativePath` — the same check `Materialize` and candidate capture already share; this is the first time it protects a write into the real, permanent workspace instead of a disposable one).

Every entry is read and validated **before** anything is written — the same guarantee `snapshot.Capture` already gives its own writes. A refused candidate (a corrupted blob, an unsafe path) leaves no partial change in the real workspace; a write never starts on entry *N-1* before entry *N* is found to be bad.

Only once every entry has passed does Apply actually write them, each one atomically (a temporary file in its own target directory, then renamed into place — the same technique every state file in this codebase already uses for itself).

The snapshot directory is the original capture for a receipt whose attempt is 1, the correction capture under `review/correction-snapshot/` for attempt 2 — `review.SnapshotDirectory` is the one shared helper, reused by this package and by `ReceiptStatus`, instead of a third inline copy of that branch.

## Idempotency, partial writes, and recovery — by construction, not a journal

Nothing here needs a write-ahead log, a pending/done list, or any bookkeeping beyond one completion marker, because of two properties that hold by construction:

- **Content is deterministic.** The bytes to write are entirely decided by the already-captured, already-verified snapshot. Writing the same bytes to the same path twice is always a safe no-op in effect.
- **Each write is atomic.** A crash can leave a file either fully old or fully new — never half-written.

That means recovery from any interruption — mid-file, between files, or right after the last file but before the completion marker — is the same single mechanism: call `Apply` again. It re-verifies and re-writes every entry, converging on the identical correct end state regardless of where the previous run stopped. There is no ambiguous state to diagnose and no partial-write bookkeeping to get wrong.

`apply.json` exists purely to make a *repeat* call cheap once a workflow is already fully applied: if its `target` already matches the current receipt's, `Apply` returns immediately without touching the filesystem again. Unlike every other artifact review and receipt persist, this record is deliberately **mutable** — the same reasoning `workflow.json` already uses — because it is a completion marker, not evidence.

## Non-authority boundary

Apply never invokes git, in any form: no stage, no commit, no push, no pull request, no merge, no release. It only ever writes ordinary files inside the workspace. This is not a check it performs — it is simply code that never calls git at all, verified by an executable test that runs Apply inside a real git repository and asserts nothing becomes staged or committed as a result.

Delivery — turning applied files into a commit, a pushed branch, an opened pull request — remains an entirely separate, human-driven decision outside this package, exactly as every roadmap note about receipt authority has said since before this layer existed.

## Persisted evidence

```text
.koard/state/runs/<workflow>/
├── workflow.json
├── consent.json
├── apply.json             # only once Apply has completed
├── snapshot/
└── review/
    ├── review.json
    ├── receipt.json
    └── ...
```

## CLI

```bash
koard review apply --workflow <id>
```

Requires nothing beyond the workflow having a valid, approved receipt. `koard review status`/`run` print one `apply: applied` line once a completed apply record exists, alongside the receipt line they already show.

## Verification

```bash
go test -race ./internal/core/apply
go test -race ./internal/cli
```

Core tests cover: refusing to apply without a verified receipt (nothing written); writing the exact receipted bytes; a second call being a genuine no-op (proved by pointing it at a workspace that does not even exist and confirming it still succeeds); recovering correctly after a simulated interruption; refusing a corrupted snapshot before writing anything; and — executed against a real git repository — never staging or committing anything.
