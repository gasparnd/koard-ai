# Risk assessment

Objective: classify each candidate snapshot into a passive, medium, or high risk tier with stable reason codes, so later review depth can follow the tier. Branch: `task/2.3-risk-assessment` from `docs/fix-step-2-merge-leftovers` (`8a50f16`). Delivery: `ask-on-risk`; forecast: about 550 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "Candidate risk assessment with passive, medium, and high tiers."
2. S2 — Koard owns the tiers and reason codes below, with closed, documented lists that never come from repository configuration. Store the assessment beside the candidate. The original decision and research provenance are preserved in the historical log.
3. S3 — Koard's risk classification rules:
   - passive, `non_executable_only`: every artifact is documentation (`.md`, `.rst`), or there are no artifacts.
   - medium, `configuration_change`: dependency, build, and configuration files (`go.mod`, `go.sum`, `Dockerfile`, `Makefile`, `.json`, `.yaml`, `.yml`, `.toml`).
   - medium, `executable_change`: any other artifact, including unknown types such as `.txt` or images.
   - high, `shell_source`: shell scripts (`.sh`, `.bash`, `.zsh`), files starting with a `#!` line, and CI workflows under `.github/workflows/`.
   - high, `process_boundary`: content that starts processes (a `#!` line, an `os/exec` import, `os.StartProcess`, `syscall.Exec`).
   - high, `hot_path`: a path segment whose name starts with `auth`, `secur`, `secret`, `credential`, `permission`, or `sandbox`.
   - high, `agent_escalation`: the caller cites a high-risk item from 1 to 6 with a reason; escalation only raises the tier.
   - high, `unassessable`: the snapshot cannot be loaded and verified.
4. S4 — The tier is the highest tier any artifact or escalation reaches. Reasons list only the signals of that tier, in artifact order, without duplicates.
5. S5 — `review_due` is true only for high. Size never changes the tier.
6. S6 — The assessment reads only verified snapshot bytes. An escalation with an item outside 1-6 or an empty reason is a caller error.
7. S7 — The router stores the assessment in `workflow.json` as `risk` when a candidate is recorded, and records none when there is no candidate.
8. S8 — Non-goals: commit-slice budgeting, choosing review lenses, consent, and any consumer of the tier. Commit-slice budgeting is not a roadmap commitment.
9. S9 — Public-interface tests for every observable above; update `docs/architecture.md`, `README.md`, and the roadmap; run `go test ./...` and `go vet ./...`.

## Tasks

- [x] T1 — S1-S9 — route: inline (one behavior, no writer trigger) — risk: none (nothing consumes the tier yet) — commit: pending (the user commits by hand).

## Log

Historical record only: external tool names and comparisons below document prior decisions and verification. They are not runtime dependencies or additional Koard requirements.

- L1 (2026-11-15): "Sí, replicalo", after asking "Como lo maneja gentle -ai?".
- L2 (2026-11-15): Experiment over 22 change types in scratch repositories with `gentle-ai review assess --json`. Observed: `.md`/`.rst` passive; `.txt`, `.png`, `.sql`, Go code and tests medium `executable_change`; `go.mod`, `.json`, `Dockerfile`, `Makefile` medium `configuration_change`; CI workflow high `shell_source`; shell script high `process_boundary` + `shell_source`; Go with `os/exec` high `process_boundary`; `internal/auth.go` high `hot_path`; `net/http`, `os.RemoveAll`, crypto stayed medium; 3000 changed lines stayed medium. The exact hot-path and configuration lists are not visible from outside, so the lists in S3 are Koard's closed versions.
- L3 (2026-11-15): RED observed: the `risk` package did not compile (`undefined: Assessment`) and the router tests failed to build on `Outcome.Risk`.
- L4 (2026-11-15): GREEN with `internal/core/risk`, `snapshot.ReadEntry` (shared by `Load`), and `Outcome.Risk` set beside the candidate. `go test -count=1 ./...`, `go vet ./...`, and `gofmt -l internal cmd` clean.
- L5 (2026-11-15): Rule checks in scratch copies: making unknown types passive failed six tier cases; disabling hot paths failed four tier cases and the router test. Both rules are pinned.
- L6 (2026-11-15): User requested: "Corrige esos puntos que me comentans anteas de que sigmaos con neustro roadmap". Active specifications now define Koard's rules without an external comparison. The original S2 recorded "Sí, replicalo", accepting a proposal to replicate gentle-ai's observed tiers and reason codes; S3 identified the observed version as 4.5.0. That provenance remains historical, not a dependency or a commitment to future compatibility. No classification behavior changed.
