# Reject a stale candidate whose snapshot no longer matches the reviewed one

Objective: detect and refuse when the workflow's current candidate no longer matches the one a receipt was issued for. Branch: `task/2.6-receipt`, same branch as the receipt milestone (extends it directly). Delivery: inline, single work unit. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Reject a stale candidate whose snapshot no longer matches the reviewed one." (`docs/roadmap.md`).
2. S2 — User continuation: after completing the receipt milestone, user chose this item over "Verify snapshot integrity, reviewer independence, consent binding, and correction limits" (the other remaining unchecked item in the same roadmap section).
3. S3 — Concrete mechanism (found by investigation, not guessed): `workflow.json` is the one artifact in this codebase that is **not** write-once — `agentrouter`'s `settle` persists it with a plain atomic rename, by design. Every other artifact (`consent.json`, `review.json`'s attempt files, `receipt.json`) is write-once. A receipt frozen at approval time could keep attesting to a candidate `workflow.json` no longer names, if the workflow record drifted afterward.
4. S4 — Detection: `review.ReceiptStatus` re-resolves the workflow's current candidate through `consent.Status(runDir)` — the exact same fresh-from-disk resolution `RunInitial` itself already used to start the review — and requires it to still equal the receipt's `target`. Reuse, not reimplementation: `consent.Status` already fails closed whenever the live workflow record no longer matches what was granted and reviewed.
5. S5 — Scope: this check applies only when the receipt's `attempt` is 1. A correction's revised candidate (attempt 2) was never declared by `workflow.json` — it is produced and owned entirely, immutably, by the review package — so there is no external record for it to drift from, and applying the check there would be a false positive on every successful correction.
6. S6 — Error reporting: a mismatch, or any error resolving the live workflow record, is reported through `ReceiptStatus` as the candidate being stale, distinct from the existing tamper-detection and lifecycle-mismatch errors.
7. S7 — Non-mutation boundary: detecting staleness never applies files, mutates the workspace, or authorizes commit, push, pull request, merge, or release.
8. S8 — Tests: a receipt whose workflow record has drifted (an independently valid but different risk tier, with the candidate and snapshot bytes untouched) is rejected as stale; a correction (attempt 2) receipt is unaffected by the same drift. CLI: the error surfaces through `koard review status`/`run`.
9. S9 — Docs: `receipt.md` (new "Staleness" section), `architecture.md`, `roadmap.md`, `README.md`. Run focused race tests, `go test ./...`, `go vet ./...`, `gofmt -l internal cmd`, and `git diff --check`.

## Tasks

- [x] T1 — S1-S9 — route: inline (one small, well-scoped extension of `ReceiptStatus`; no writer trigger) — risk: item 3 (changes what an existing public verification function accepts) — commit: this work-unit commit; RED: no staleness check existed; GREEN: focused race tests, suite, vet, formatting, diff checks pass.

## Log

- L1 (2026-11-16): After completing the receipt milestone, asked which of the two remaining roadmap items to do next. User chose "Rechazar candidato stale (Recomendado)" over the broader integrity/independence/consent/correction-limits verification item.
- L2 (2026-11-16): Investigated what "stale" could concretely mean in the current codebase before writing anything (no prior design existed). Found that `workflow.json` is the only non-write-once artifact in the system, which is exactly what the roadmap's own note explains ("the candidate identity binds declared references, not the bytes an immutable snapshot will capture"). This made the gap precise and implementable without needing to ask the user anything further: it is purely a matter of reusing `consent.Status`'s existing fresh-resolution check from inside `ReceiptStatus`.
- L3 (2026-11-16): Implemented T1. Also confirmed structurally that a workflow run directory has exactly one `snapshot/manifest.json`, so "swap to a different but still fully valid alternate snapshot" cannot be simulated by changing `Candidate.ID`/`SnapshotID` alone (those cross-validate against the stored manifest and would instead surface as an existing corruption/mismatch error); the realistic, fully-valid drift scenario is changing `Risk.Tier` alone, which nothing else cross-validates — used for the test. `go build`, `go vet ./...`, `gofmt -l internal cmd`, `go test -race ./...`, and `git diff --check` all pass.
- L4 (2026-11-16): Commit `4a3a37a` (T1, the whole milestone) assessed `medium` risk, `review_due: false` (`under_budget`, 147 lines against base `d16bf68`, the tip of the receipt milestone). `roadmap.md` marked this item done.
