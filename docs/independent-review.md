# Independent review

Independent review is Koard's read-only checkpoint between a consented candidate and any future mutation capability. It asks a fresh reviewer to inspect the exact bytes the user authorized—not whatever happens to be in the workspace later.

The core lifecycle, the OpenCode-backed reviewer and corrector, and the CLI surface all exist now: `koard review run`, `koard review status`, and `koard review correction`.

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
| Provider adapter | Runs a fresh reviewer (and, for a correction, a corrector) with read-only capabilities over a materialized snapshot | Core contracts stay portable across runtimes; the live workspace is never what gets inspected |
| Correction consent | Separately authorizes one proposal-only correction | Review permission never silently becomes correction permission |
| CLI | Exposes `run`, `status`, `correction` over the exact run directory | The only way a human or script touches this lifecycle |
| Receipt | Immutable, digest-verifiable proof that a lifecycle reached `approved` | A future layer never has to replay history to answer "was this reviewed?" |

## Initial review path

1. Resolve the completed workflow and its consent record.
2. Require `granted` consent for the exact workflow, candidate, snapshot, and risk tier.
3. Recompute the snapshot identity and verify every stored blob.
4. Require that review mode is currently `on`.
5. Claim `.koard/state/runs/<workflow>/review/`: build `attempts/` and the initial `review.json` in a sibling temporary directory first, then publish both with one atomic rename. A reader or a second claimant only ever sees no review directory or the fully formed one, never one half-written by a crash in between.
6. Persist the immutable request under `review/attempts/1-request.json`.
7. Require again that review mode is still `on`, immediately before invoking the reviewer — a revocation during claim preparation, or while a slow provider is starting, must still stop the attempt.
8. Invoke a fresh reviewer with only `repository:read`, over the exact snapshot materialized into a disposable temporary directory — never the live workspace, which may already have drifted from what was consented to.
9. Validate the result against the request identity, attempt, target, and snapshot evidence.
10. Persist the immutable result and update `review/review.json`.

The result can approve, reject, or request correction. A correction request pauses the lifecycle. It does not run a corrector and does not change workspace files by itself.

## Correction path

A paused, `correction_required` lifecycle stays paused until a **separate** immutable decision answers the exact pending correction request — review consent never implies correction authority.

1. `DecideCorrection` records `granted` or `declined` for the exact `correction_request_id` the lifecycle is paused on. Repeating the same decision is idempotent; a conflicting one fails closed.
2. `RunCorrection` requires that decision to exist for the exact paused request before doing anything else.
3. **Declined**: the lifecycle settles as `rejected`. No provider ever runs.
4. **Granted**: review mode must be `on` (checked live, the same as the initial attempt). Since nothing has been claimed or persisted yet at this point, a revocation here leaves the lifecycle unchanged and retryable — it does not burn the one permitted correction.
5. A fresh corrector is handed a read-only (`repository:read`) handoff describing the findings and the original snapshot's artifacts, and proposes a revision — exactly the same `Runner`, `candidate.Produce`, and `snapshot.Capture` primitives the initial workflow candidate already uses, reused rather than duplicated.
6. The proposal is captured into its own immutable snapshot under `review/correction-snapshot/` and re-assessed for risk, producing a revised target (new candidate and snapshot identity, same workflow).
7. That revised target is independently re-reviewed as attempt 2, with its own immutable `2-request.json` / `2-result.json`, exactly like the first attempt.
8. Approved or rejected settles the lifecycle. A second `correction_required` verdict **escalates** instead of pausing again — `MaxCorrections` permits exactly one, so no loop is possible, and no second correction-request file is ever written.

## Receipt

The instant — and only the instant — either path above settles `approved`, it issues one immutable, digest-verifiable receipt bound to that exact candidate and verdict. See `receipt.md` for its content, verification, and why it carries no cryptographic signature.

## Persisted evidence

```text
.koard/state/runs/<workflow>/
├── workflow.json
├── consent.json
├── snapshot/
└── review/
    ├── review.json
    ├── receipt.json                  # only once the lifecycle settles approved
    ├── correction-request.json       # only when correction is requested
    ├── correction-decision.json      # only once a separate decision is recorded
    ├── correction-snapshot/          # only once a granted correction proposes a revision
    └── attempts/
        ├── 1-request.json
        ├── 1-result.json
        ├── 2-request.json            # only after a granted correction is re-reviewed
        └── 2-result.json
```

`review.json` is the current state. Attempt files are immutable evidence. A claimed lifecycle always leaves a diagnosable state, including provider and validation failures.

## Invariants

- Review starts only after exact granted consent and successful snapshot verification.
- Review-mode authority is checked live, not once and assumed: it is re-read before the claim and again right before each reviewer or corrector invocation, so a revocation mid-attempt is never missed.
- The claim itself is all-or-nothing: `attempts/` and `review.json` are staged together and published with one directory rename, so a crash can never leave a half-claimed lifecycle that blocks every future attempt without explanation.
- A review-mode revocation caught before anything has been persisted for the current step (the initial claim, or the start of a correction) leaves the lifecycle unchanged and retryable; one caught after a claim or a provider has already run settles the lifecycle as `failed`, so the distinction is always diagnosable from persisted state.
- Reviewer identity comes from trusted application configuration, never model output.
- The reviewer and the corrector each receive only `repository:read`.
- Findings may cite only URI and digest pairs present in the snapshot they review.
- An approved result cannot contain blocking findings.
- Concurrent or replayed claims never interleave: the loser's rename is rejected because the destination is already a complete, non-empty directory, and it is reported the same way as any other already-claimed lifecycle.
- Review never applies files or authorizes commit, push, pull request, merge, or release — a correction's revised candidate is itself just another immutable, read-only-captured snapshot.
- Correction requires a separate immutable decision bound to the exact review target and the exact correction request, and remains proposal-only.
- Exactly one correction is ever possible: a second `correction_required` verdict escalates rather than pausing, and `DecideCorrection` only accepts a decision while the lifecycle is paused for that exact request.

## CLI

```bash
koard review run --workflow <id>        # executes only the next authorized bounded transition
koard review status --workflow <id>     # read-only: state, attempt, verdict, correction budget
koard review correction status --workflow <id>
koard review correction grant --workflow <id>
koard review correction decline --workflow <id>
```

`run` resolves the current lifecycle itself: with no review yet, it runs the initial attempt; paused for correction with a decision already recorded, it runs that one correction; paused without a decision, it refuses and names the missing step; already settled (`approved`, `rejected`, `failed`, `escalated`), it refuses — there is nothing left to run. `status` and `correction status` never write anything.

The reviewer and corrector are the project's existing `agents.verifier` OpenCode configuration — independent review adds no config schema of its own for this milestone. `Materialize` (in the `snapshot` package) turns the exact reviewed snapshot back into an ordinary file tree in a disposable directory before each reviewer or corrector invocation, so a generic read-only coding agent can inspect it with its own tools without ever touching the live, possibly-drifted workspace; the directory is removed as soon as the invocation returns.

## Failure behavior

Koard fails closed before provider invocation when consent, snapshot, or review-mode state is absent, declined, malformed, mismatched, or unverifiable. After a lifecycle is claimed, provider failures, a mid-attempt review-mode revocation, and invalid results all become terminal `failed` records so operators can distinguish “never started” from “started but failed.”

## Verification

The core tests use isolated temporary workspaces and fake reviewers/correctors to prove exact binding, read-only capabilities, immutable evidence, correction pause, separately granted and declined correction decisions, one correction and re-review, second-correction escalation, provider failure persistence, and concurrent claim refusal:

```bash
go test -race ./internal/core/review
go test -race ./internal/adapters/opencode
go test -race ./internal/core/snapshot
go test -race ./internal/cli
```
