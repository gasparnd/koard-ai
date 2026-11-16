# Verify snapshot integrity, reviewer independence, consent binding, and correction limits

Objective: close the gap-analysis for this roadmap item — document which existing check already covers each of the four named properties, and add the direct tests that were missing. No new runtime surface. Branch: `task/2.6-receipt`, same branch as the receipt and stale-candidate work. Delivery: inline, single work unit. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Verify snapshot integrity, reviewer independence, consent binding, and correction limits." (`docs/roadmap.md`).
2. S2 — User continuation: after stale-candidate rejection, user asked to start this, the last remaining item in the "Receipt-driven development" roadmap section.
3. S3 — Scope decision: user chose "Cerrar el gap-analysis (Recomendado)" over a new dedicated `koard review verify` command. Document which existing check covers which property, add only the missing direct tests, no new public surface.
4. S4 — Gap analysis (by investigation): three of the four properties were already fully covered with direct tests (snapshot integrity for attempt 1, consent binding including staleness, the happy-path correction-limit test). Three concrete, real gaps were found and closed:
   - Snapshot integrity for a **correction's** snapshot (attempt 2) had no corruption test — only the original (attempt 1) did.
   - Reviewer independence's "no session continuation" guarantee was asserted for the executor/proposal path (`TestAdapterBuildsProposalOnlyInvocation`) but never for the review path's own invocation.
   - Correction limits were tested for the happy path (escalation on a second `correction_required`) but never for a caller trying a **third** attempt or decision after escalation.
5. S5 — Non-mutation boundary: this item only adds tests and documentation; it changes no runtime behavior.
6. S6 — Docs: a "Verification matrix" section in `independent-review.md` naming, for each of the four properties, exactly where it is checked and which test would catch a regression. `roadmap.md` marked done.

## Tasks

- [x] T1 — S1-S6 — route: inline (closing three small, independent test gaps plus documentation; no writer trigger) — risk: none (tests and docs only, no production code changed) — commit: this work-unit commit; RED: the three gaps named in S4 had no test; GREEN: focused race tests, suite, vet, formatting, diff checks pass.

## Log

- L1 (2026-11-16): After the stale-candidate milestone, user asked to start the last remaining roadmap item in this section too. Asked whether it should produce a new dedicated verification command/report, or close the gap against what already exists; user chose the latter.
- L2 (2026-11-16): Mapped all four properties against the actual code before writing anything (see S4). Found the codebase's own type design already structurally guarantees reviewer independence (`handoff.Result`/`review.Result` have no reviewer field at all — the model cannot report its own identity), which made clear this item was mostly about proving coverage, not adding mechanism.
- L3 (2026-11-16): Added `TestReceiptStatusDetectsCorruptedCorrectionSnapshot`, extended `TestAdapterReviewMaterializesSnapshotReturnsBoundResultAndCleansUp` with the same forbidden-flag assertion the proposal path already had, and added `TestRunCorrectionRefusesAThirdAttemptAfterEscalation` / `TestDecideCorrectionRefusesAfterEscalation`. `go build`, `go vet ./...`, `gofmt -l internal cmd`, `go test -race ./...`, and `git diff --check` all pass. This completes every item in the roadmap's "Receipt-driven development" section.
- L4 (2026-11-16): Commit `174b203` (T1, the whole milestone) assessed `medium` risk, `review_due: false` (`under_budget`, 143 lines against base `ffac80b`). Native review has still not fired once across the entire 2.5+2.6 branch chain.
