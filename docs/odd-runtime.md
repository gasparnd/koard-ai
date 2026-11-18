# Guided ODD through authorized apply

`koard odd run` starts a read-only proposal workflow and records ODD preparation.
`koard odd status` derives progress from persisted proposal, consent, review,
receipt and apply records. **A verified proposal is not completed ODD work:**
completion means application was registered for the exact currently verified receipt.
Commit, push, pull request, merge and release remain separate human actions.

## Quick path

Configure providers as described in [routing.md](routing.md), with
`workflow.default: odd`, then run:

```sh
koard odd run --workflow example --task example --objective 'Propose a bounded change' --policy planned
koard odd status --workflow example
```

The output shows the next explicit action. Choose review consent with the existing
`koard review consent grant|decline --workflow example` commands. A grant permits
`koard review run --workflow example`; it does not run a reviewer by itself. A
correction requires its separate `review correction grant|decline` decision and a
subsequent `review run`. An approved receipt permits the explicit command
`koard review apply --workflow example`. Query ODD status afterward to see closure.

Grant and decline are alternatives. ODD never selects either, enables review,
invokes review/apply on a status query, retries a provider, or reruns a claimed ID.
It uses the existing guards, including the one-correction limit. Review disabled
without a receipt leaves the workflow blocked; a receipt already verified remains
eligible for explicit apply under existing policy, even when review is later disabled.

Both `fixed` and `planned` retain their existing role routing. Memory recall and
retention reuse ordinary `koard run` composition. Existing `run` behavior is
unchanged and creates no ODD metadata. Missing `odd.json` is reported as a non-ODD
run, not retroactively reconstructed preflight.

## Phase semantics in this bounded runtime

| Phase | Actual evidence / boundary |
|---|---|
| Authorize | Explicit objective and task authorize only a read-only proposal; later authority remains in existing decisions. |
| Explore | Inspect workspace and strictly parse configuration; record its reference and digest. This is technical exploration, not semantic product research. |
| Resolve | Resolve supported providers/models for the route. Business ambiguity is not automatically resolved. Models can return `blocked`. |
| Classify | Record `fixed`/`planned` and required roles. Candidate risk is assessed later from the captured snapshot. |
| Implement | Produce and verify the proposal, then wait for explicit review/correction/apply. |
| Close | Verify the receipt and a matching registered apply target; report completion of this scope, not delivery. |

Failed preflight starts no model and creates no ODD preparation. The router's
exclusive claim precedes publication of `odd.json`; failed publication settles
the claimed run as failed before a handoff. A claimed run is never resumed by
`odd run`. Proposal-process crash recovery and a business-planning conversation
are outside this version.

## Reading progress

| Reason | ODD state | Next step |
|---|---|---|
| `consent_required` | blocked at implement | Exact candidate consent decision |
| `review_disabled` | blocked at implement | User decides whether to enable review; no automatic settings change |
| `review_ready` | implement | Explicit `review run` |
| `correction_consent_required` | blocked at implement | Separate correction decision |
| `correction_ready` | implement | Explicit `review run`, at most one correction |
| `apply_required` | implement | Explicit `review apply` |
| `applied` | completed | No automatic delivery |
| `consent_declined`, `correction_declined`, `review_rejected` | stopped | No apply; a declined correction may be settled as rejected with `review run` |
| `proposal_failed`, `proposal_blocked`, `proposal_incomplete`, `review_failed`, `review_reviewing`, `review_escalated` | blocked | Inspect retained diagnostics; no inferred retry or resume |
| `proposal_cancelled` | stopped | No further proposal execution |

`review_reviewing` can mean active work or an interrupted attempt; a read cannot
distinguish those safely. Corrupt evidence or unreadable review-mode settings are
errors, not assumed declines, disabled mode, or successful completion.

## Persistence and authority

`internal/application/oddworkflow` composes core contracts above router and review;
it imports no runtime adapter. `odd.json` is an immutable versioned technical-
preflight checkpoint at `implement`, built through `odd.Machine.Apply`. Status
applies legal transitions to that checkpoint using current evidence, without
writing. Its derived revision is not a chronological event count or execution log.
Existing handoffs, `events.jsonl`, review attempts and decisions retain that role.

There is no mutable ODD authority sidecar to synchronize after apply, and no
checkpoint failure can cause ODD to repeat a model or roll back applied changes.
Each query validates workflow binding and persisted handoff bytes, then reuses
consent/lifecycle/receipt/apply readers. Correction receipts name the corrected
target. A mismatching apply marker is an error rather than completion.

An apply record establishes registered application, not perpetual workspace
integrity or cryptographic provenance. Local state is not tamper-proof. Status
does not claim files remained unchanged afterward. Its guidance grants no authority:
each invoked command revalidates its own guards. A concurrent inconsistent read
may refuse; querying again has no write effects.

## Verification

Public CLI tests cover both policies, consent and review stops, correction grant/
decline/escalation, corrected-target closure, malformed evidence, legacy behavior,
read-only queries and provider counts. Tests use fake providers, temporary workspaces
and isolated settings; no live model or shared Engram store. Router tests cover
failed initialization before provider work.

```sh
go test ./internal/application/oddworkflow ./internal/core/agentrouter ./internal/cli
go test ./...
go vet ./...
```
