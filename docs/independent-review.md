# Independent review

Independent review is Koard's read-only checkpoint between a consented candidate and any future mutation capability. It asks a fresh reviewer to inspect the exact bytes the user authorized—not whatever happens to be in the workspace later.

The core lifecycle exists now. The CLI and OpenCode wiring remain part of the active roadmap milestone and are not yet available as user commands.

## Why this layer exists

A candidate identity explains **what was proposed**. A snapshot binds **which bytes support that proposal**. Consent records **which exact candidate the user permits Koard to review**. Independent review then answers a different question: **does a fresh reviewer approve those exact bytes?**

Keeping these questions separate prevents one broad permission from silently becoming mutation or delivery authority.

## Layer map

| Layer | Responsibility | Why it matters |
|---|---|---|
| Candidate | Gives the proposal a deterministic identity | The same proposal can be recognized across processes |
| Snapshot | Stores and verifies the referenced bytes | Review cannot drift to newer workspace content |
| Risk | Classifies the candidate before review | Policy can react to consequence without changing identity |
| Consent | Grants review authority for one exact target | The user controls which candidate may be inspected |
| Review lifecycle | Claims one run, emits a bound request, validates the result, and persists the transition | Providers cannot invent authority or skip states |
| Provider adapter | Runs a fresh reviewer with read-only capabilities | Core contracts stay portable across runtimes |
| Correction consent | Separately authorizes one proposal-only correction | Review permission never silently becomes correction permission |

## Initial review path

1. Resolve the completed workflow and its consent record.
2. Require `granted` consent for the exact workflow, candidate, snapshot, and risk tier.
3. Recompute the snapshot identity and verify every stored blob.
4. Atomically claim `.koard/state/runs/<workflow>/review/`.
5. Persist the immutable request under `review/attempts/1-request.json`.
6. Invoke a fresh reviewer with only `repository:read`.
7. Validate the result against the request identity, attempt, target, and snapshot evidence.
8. Persist the immutable result and update `review/review.json`.

The result can approve, reject, or request correction. A correction request pauses the lifecycle. It does not run a corrector and does not change workspace files.

## Persisted evidence

```text
.koard/state/runs/<workflow>/
├── workflow.json
├── consent.json
├── snapshot/
└── review/
    ├── review.json
    ├── correction-request.json       # only when correction is requested
    └── attempts/
        ├── 1-request.json
        └── 1-result.json
```

`review.json` is the current state. Attempt files are immutable evidence. A claimed lifecycle always leaves a diagnosable state, including provider and validation failures.

## Invariants

- Review starts only after exact granted consent and successful snapshot verification.
- Reviewer identity comes from trusted application configuration, never model output.
- The reviewer receives only `repository:read`.
- Findings may cite only URI and digest pairs present in the reviewed snapshot.
- An approved result cannot contain blocking findings.
- Concurrent or replayed claims never interleave.
- Review never applies files or authorizes commit, push, pull request, merge, or release.
- Correction requires a separate immutable user decision and remains proposal-only.

## Failure behavior

Koard fails closed before provider invocation when consent or snapshot state is absent, declined, malformed, mismatched, or unverifiable. After a lifecycle is claimed, provider failures and invalid results become terminal `failed` records so operators can distinguish “never started” from “started but failed.”

## Verification

The core tests use isolated temporary workspaces and fake reviewers to prove exact binding, read-only capabilities, immutable evidence, correction pause, provider failure persistence, and concurrent claim refusal:

```bash
go test -race ./internal/core/review
```
