# Candidate-bound review consent

Objective: let the user grant or decline review consent for one exact persisted candidate snapshot, without authorizing workspace mutation or delivery. Branch: `task/2.4-candidate-consent`. Delivery: `ask-on-risk`; forecast: about 650 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Explicit consent bound to the exact candidate snapshot."
2. S2 — User authorization: "empecemos" after the proposed scope: show the candidate, its risk, and what review is authorized; persist acceptance or rejection bound to `candidate_id + snapshot_id`; reject a decision for another identity; keep rejection candidate-scoped; and prove the path `candidate → snapshot → risk → consent` through a public interface.
3. S3 — User-selected interface: "Por workflow (Recommended)". The public command is `koard review consent <status|grant|decline> --workflow <id>` and resolves `.koard/state/runs/<id>/workflow.json` from the current Koard project.
4. S4 — `grant` and `decline` load a completed workflow with a candidate, require non-empty candidate and snapshot identities, require a risk assessment bound to the same snapshot, and verify the immutable snapshot before recording a decision.
5. S5 — The versioned consent record stores the workflow, candidate, and snapshot identities; decision; risk tier; and the exact authority `review`. Grant authorizes only independent review of that snapshot. It never authorizes correction, apply, commit, push, pull request, merge, or release.
6. S6 — Consent is immutable for one workflow. Repeating the same decision is idempotent; a conflicting decision or any target mismatch fails closed without replacing the record. A decline affects only that candidate snapshot and never changes review mode.
7. S7 — `status` is read-only and reports the exact target and recorded decision, or that no decision exists. Grant and decline refuse when effective review mode is off; status remains available.
8. S8 — The record is written atomically under the workflow run directory. Strict decoding rejects unknown fields, unsupported schema versions, trailing data, malformed identities, unknown decisions or authorities, and mismatches with the current workflow record.
9. S9 — Public-interface tests cover status, grant, decline, idempotency, conflicting decisions, review-mode off, missing or invalid workflow state, binding mismatches, snapshot verification failure, usage errors, and the absence of workspace mutation.
10. S10 — Update CLI help, README, architecture, focused consent documentation, docs index, and roadmap. Run `go test ./...`, `go vet ./...`, and `gofmt -l internal cmd`.

## Tasks

- [x] T1 — S1-S8 — route: inline (dependent core contract; no writer trigger) — risk: item 2 (consent is a security/authority guard) — commit: this work-unit commit; RED: package symbols absent; GREEN: focused core tests, race test, suite, vet, formatting, and independent verification pass.
- [ ] T2 — S3, S7, S9-S10 — route: inline (public integration depends on T1; no writer trigger) — risk: item 2 (CLI grants review authority) — commit: pending.

## Log

- L1 (2026-11-15): "empecemos".
- L2 (2026-11-15): Before implementation, the user authorized closing milestone 2.3 and creating a new branch: "dale". External work had already committed and published 2.3 as `f80482f`; the local bookkeeping commit is `9906492`. Branch `task/2.4-candidate-consent` starts there.
- L3 (2026-11-15): User selected "Por workflow (Recommended)" for the consent interface.
- L4 (2026-11-15): Assumptions for fail-closed authority: one immutable candidate-scoped decision per workflow; same-decision replay is idempotent; changing the decision requires a new workflow/candidate; review mode off refuses new decisions but does not hide status. These assumptions preserve the existing user-owned mode and do not expand consent into mutation authority.
- L5 (2026-11-15): T1 RED was the expected compile failure for the absent consent contract. GREEN adds strict workflow and consent decoding, verified snapshot and risk binding, immutable atomic publication, idempotent replay, conflicting-decision refusal, and shared workflow-ID validation. An independent verifier passed S1 and S4-S6/S8, confirmed no mutation or delivery authority, and supplied a concurrent conflicting-decision probe that was added as a regression test. T2 remains intentionally open for CLI, review-mode enforcement, public tests, and docs.
