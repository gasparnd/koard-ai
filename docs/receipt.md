# Receipt

A receipt is immutable, verifiable proof that one exact candidate snapshot completed the independent-review path: granted consent, an approved verdict, and the correction budget it spent. It exists so a later layer — controlled mutation — never has to re-ask "was this actually reviewed?"; it just checks one small, self-contained file.

## Why this layer exists

Review produces a `Lifecycle` that can change state and, after a correction, even change which candidate it is about. A receipt is the opposite: a frozen, content-addressed snapshot of the one moment the lifecycle reached `approved`. Separating "the lifecycle as it evolves" from "the proof that it once reached approval" means a future consumer never has to replay history to answer a yes/no question.

## What "verifiable" means here

The receipt is **not** cryptographically signed. There is no private key, no key rotation, no external trust anchor. "Verifiable" means exactly what it means for every other artifact in this codebase — `candidate.Candidate`, `snapshot.Manifest`, `consent.Record` — content addressing: the receipt's `id` is the SHA-256 digest of its own canonical content. Anyone with filesystem access can recompute that digest and know whether the receipt was tampered with, and can cross-check its `target` against the stored lifecycle and the snapshot it names.

This is a deliberate, scoped choice. A cryptographic signature would let the receipt be trusted outside the local filesystem boundary (a different machine, a CI system that never ran the review itself) — that is not a problem this milestone solves, and adding one would mean building key management infrastructure the project does not have yet.

## Issuance

A receipt is issued **automatically**, inside the review lifecycle itself, the instant — and only the instant — a lifecycle settles `approved`:

- From the initial attempt (`RunInitial`), or
- From the one permitted correction (`RunCorrection`), for the revised candidate.

No separate command, flag, or consent authorizes it; there is nothing to decide, because everything the receipt records is already true the moment the verdict is approved. Every other terminal state — `rejected`, `failed`, `escalated` — never gets one. If issuing the receipt itself fails, the lifecycle settles `failed` instead of silently becoming `approved` without one: an approved state is never persisted unless its receipt is too.

## Content

```json
{
  "schema_version": 1,
  "id": "sha256:...",
  "target": { "workflow_id": "...", "candidate_id": "sha256:...", "snapshot_id": "sha256:...", "risk_tier": "medium" },
  "reviewer": { "runtime": "opencode", "model": "..." },
  "attempt": 1,
  "max_corrections": 1,
  "corrections_used": 0
}
```

`target` is the exact workflow, candidate, snapshot, and risk tier that was approved — the revised one when `attempt` is 2. `reviewer` is trusted application configuration, never model output. `max_corrections`/`corrections_used` record exactly how much of the one-correction budget this approval spent.

## Persisted evidence

```text
.koard/state/runs/<workflow>/review/receipt.json
```

Sibling to `review.json`, inside `review/` — not at the run-directory root — because a receipt only exists in relation to one specific review lifecycle, the same lifecycle `review.json` and `attempts/` already describe.

## Verification

`review.ReceiptStatus(runDir)` is the read-only check:

1. A missing receipt is not an error — most lifecycles never reach `approved`. It returns `nil, nil`.
2. A receipt that exists must be structurally valid (schema version, well-formed target, valid reviewer, correction budget in range).
3. Its `id` must match the SHA-256 digest recomputed from its own content — tamper detection.
4. Its `target`, `reviewer`, `attempt`, and `corrections_used` must match the lifecycle currently stored at `review.json` — a receipt that no longer matches the lifecycle it was issued for is refused, not silently trusted.
5. For attempt 1 only: the workflow's candidate must still be exactly what this receipt was issued for — see "Staleness" below.
6. The exact snapshot it names must still load and verify — the original snapshot for attempt 1, the correction snapshot under `review/correction-snapshot/` for attempt 2.

Any failure at any of these steps fails closed with a specific error; nothing here ever repairs or re-issues a receipt.

## Staleness

Unlike every other artifact this receipt depends on, `workflow.json` is **not** write-once — `agentrouter`'s `settle` replaces it with a plain atomic rename, by design, since a workflow's own record can legitimately need rewriting. That asymmetry is exactly the gap the roadmap calls out: *"the candidate identity binds declared references, not the bytes an immutable snapshot will capture."* A receipt frozen at approval time would otherwise keep attesting to a candidate `workflow.json` no longer actually names, if the workflow record ever drifted afterward (a bug, a manual edit, or a future replay).

`ReceiptStatus` closes this by re-resolving the workflow's *current* candidate through `consent.Status` — the exact same fresh-from-disk resolution `RunInitial` itself used to start the review — and requiring it to still equal the receipt's `target`. `consent.Status` already fails closed whenever the live workflow record no longer matches what was granted and reviewed (its own consent-binding check), so this is reused, not reimplemented. A mismatch, or any error resolving the live record, is reported as the reviewed candidate being **stale**.

This check applies only to attempt 1. A correction's revised candidate (attempt 2) was never declared by `workflow.json` in the first place — it is produced and owned entirely by the review package itself, immutably — so there is no external record for it to drift from.

## Invariants

- A receipt exists if and only if its lifecycle's current state is `approved`.
- Issuing a receipt never applies files, mutates the workspace, or authorizes commit, push, pull request, merge, or release — it only proves a candidate passed the required path; it never acts on that proof.
- A receipt is never re-issued or edited: approving a lifecycle issues exactly one, matching the one-shot nature of the review attempt that produced it.
- Verification always re-derives trust from the receipt's own bytes and the independently stored lifecycle and snapshot; it never trusts the receipt's claims about itself.

## CLI

`koard review status --workflow <id>` and `koard review run --workflow <id>` print a `receipt: <id>` line whenever the lifecycle is `approved` — no dedicated receipt subcommand exists, because there is nothing to decide or configure about a receipt that the lifecycle status does not already answer.

## Verification (tests)

```bash
go test -race ./internal/core/review
go test -race ./internal/cli
```

Core tests cover: issuance exactly on approval (initial and post-correction), absence on every other terminal state, tamper detection, lifecycle-mismatch detection, corrupted-snapshot detection, stale-candidate rejection when `workflow.json` drifts, and that a correction receipt is unaffected by that same drift check. See `independent-review.md` for the review lifecycle this receipt is tied to.
