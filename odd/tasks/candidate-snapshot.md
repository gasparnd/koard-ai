# Candidate snapshot

Objective: capture the exact bytes a candidate proposes, so later review, consent, and receipts bind content instead of declared references. Branch: `task/2.2-candidate-snapshot` from `step/2-receipt-driven-development`. Delivery: `ask-on-risk`; forecast: about 500 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Immutable candidate snapshot and identity binding."
2. S2 — User decision: "Registro aparte (Recommended)": "Cuando el executor termina, el router copia los bytes de cada artefacto `repo://` (relativo al workspace, sin escapes ni symlinks que salgan) a blobs direccionados por contenido en el estado del run, y escribe un manifest. La identidad del snapshot es el SHA-256 del manifest (ID del candidato + digest de cada archivo). El ID del candidato no cambia y gana un `snapshot_id`. Un artefacto que no sea `repo://`, o un digest declarado que no coincida, rechaza el candidato. La evidencia sigue siendo solo declarada."
3. S3 — Storage: blobs at `<run>/snapshot/blobs/sha256/<hex>`, read-only, and the manifest at `<run>/snapshot/manifest.json`. The manifest lists, in artifact order, each artifact's kind, URI, `sha256:` digest of the captured bytes, and size, plus the candidate ID and a schema version.
4. S4 — A `repo://` path must be relative, stay inside the workspace after resolving symlinks, and name a regular file. Anything else refuses the candidate.
5. S5 — A refused snapshot fails the run: status `failed`, no candidate in `workflow.json`, and a `KOARD_SNAPSHOT_REFUSED` error diagnostic naming the executor handoff and the reason.
6. S6 — The candidate ID is unchanged by the snapshot; `snapshot_id` is recorded beside it. Identical artifacts and bytes for the same candidate produce the same snapshot ID; different bytes behind the same reference produce a different one.
7. S7 — A stored snapshot can be loaded and verified: every blob must match its digest and the manifest must match the snapshot ID, otherwise loading fails.
8. S8 — The router now requires a workspace directory, because a snapshot reads from it. Reading it is within `repository:read`; the router still writes only under its state directory.
9. S9 — Non-goals: snapshotting evidence, giving the verifier the snapshot reference, stale-candidate rejection, and consent binding.
10. S10 — Public-interface tests for every observable above; update `docs/architecture.md`, `docs/handoffs.md`, `docs/events.md`, `README.md`, and the roadmap; run `go test ./...` and `go vet ./...`.

## Tasks

- [ ] T1 — S1-S10 — route: inline (one behavior, no writer trigger) — risk: item 2 (path guard that keeps reads inside the workspace) — commit: pending.

## Log

- L1 (2026-11-15): "Listo, sigamos con la siguiente tarea del roadmap".
- L2 (2026-11-15): Task 2.1 is committed as `57ce7d5` on `task/2.1-review-mode` and pushed, but not yet merged into the step branch, which is not on origin. Task 2.2 does not depend on it, so it branches from the step tip `90a6810`.
- L3 (2026-11-15): Documented artifact URIs are `repo://<path>`; evidence uses `command://`. `TestRouteCarriesPreviousResultToNextHandoff` uses `koard://proposals/task-1`, which S2 now refuses, so its fixture moves to a real `repo://` file.
- L4 (2026-11-15): RED observed: the new `snapshot` package did not compile (`undefined: Capture`), and the router tests failed to build on `Options.Workspace` and `snapshot.Load`.
- L5 (2026-11-15): GREEN with `internal/core/snapshot` and router integration. One test assertion was wrong, not the guard: errors name the artifact with `%q`, which escapes a backslash, so the test now looks for the quoted URI. `go test -count=1 ./...`, `go vet ./...`, and `gofmt -l internal cmd` clean.
- L6 (2026-11-15): Guard check in scratch copies: disabling the inside-workspace check made the two symlink cases fail; disabling the syntactic path check left only `./a.go` passing, because resolution still catches traversal. Both guards are pinned.
- L7 (2026-11-15): gentle-ai assess with `--escalate-item 2`: `risk: high`, `review_due: true`, 757 changed lines across 11 paths. An independent verifier runs before the native review.
- L8 (2026-11-15): Independent verifier: S1-S10 pass, no blockers. Adopted two observations: `Load` refuses trailing data after the manifest, and a router test proves the same references with different bytes keep the candidate ID and change `snapshot_id`. Not adopted, recorded as known limits: a hard link to an outside file is captured (path checks cannot see it), a FIFO swapped in between `Lstat` and `Open` would block, and reads have no size cap.
- L9 (2026-11-15): Native review consented ("Review this change"), lineage `review-5f2e068205bfcab9`, lenses risk, resilience, reliability. The new files were marked intent-to-add (`git add -N`) because the untracked-selection schema is undocumented. All three in-process reviewer captures failed before review: the `claude` CLI reviewer transport reported "OAuth session expired and could not be refreshed". Bound STATUS re-offers the same three slots at revision `sha256:afeb70e8…`; captures are pending re-authentication.
- L10 (2026-11-15): After `claude auth login`, the three captures were admitted and the review closed `approved`; acknowledgement burned the authority (`review-5f2e068205bfcab9`). Advisory warnings, none blocking: unbounded `io.ReadAll` and all artifact bytes held in memory before writing (risk, resilience, reliability); `Capture` takes no context, so cancellation is not observed during capture (resilience, reliability); a pre-existing manifest is a hard error rather than an idempotent same-content success; one non-`repo://` artifact fails the whole run, which is S2 by design. Suggestion: a direct candidate-package test that `SnapshotID` is outside the digest.
- L11 (2026-11-15): "Sí, corregilo" for the memory and cancellation warnings. RED observed (`Capture` took no context). GREEN: `MaxArtifactBytes` 8 MiB checked on `Lstat` and on a bounded read, `MaxSnapshotBytes` 32 MiB across declared artifacts, and `Capture(ctx, ...)` checks cancellation before each read and each blob publish; the router maps a cancelled capture to `KOARD_WORKFLOW_CANCELLED` naming the executor handoff. A snapshot with no artifacts does no I/O worth cancelling, so `TestRouteStopsBetweenHandoffsWhenCancelled` is unchanged. Manifest idempotency and the direct candidate test were left out on purpose: a run directory is never reused, and `Produce` never reads `SnapshotID`.
