# Review mode

Objective: give the user a switch that decides whether receipt-driven development applies, before any review exists to consume it. Branch: `task/2.1-review-mode` from `step/2-receipt-driven-development`. Delivery: `ask-on-risk`; forecast: about 450 authored changed lines. Test runner: `go test` and `go vet`.

## Specs

1. S1 — Roadmap outcome: "User-owned `enable`, `disable`, and `status` review mode."
2. S2 — User decision: "Global + clon (Recommended)": "Igual que gentle-ai: un archivo global del usuario (directorio de config del sistema) y uno local del clon en .koard/state/ (ignorado por git). Si cualquiera de los dos dice off, gana off. Sin decisión, el default es on. status solo lee y muestra qué fuente decidió."
3. S3 — `koard review mode status` prints the effective mode, the source that decided it (`default`, `global`, or `clone`), and the value of each scope. It never creates or changes a file.
4. S4 — `koard review mode enable` and `koard review mode disable` accept `--scope clone` or `--scope global`, write that scope, and print the resulting status. Assumption: the scope defaults to `clone`, so a change outside the current project needs an explicit flag. When the requested mode is not the effective one, the output says which scope keeps it.
5. S5 — Resolution: off wins. A clone `off` decides first, then a global `off`, then a clone `on`, then a global `on`; with nothing set the mode is `on`, decided by `default`.
6. S6 — Locations: the clone setting is `.koard/state/review-mode.json` in the current directory, which must already be a Koard project for writes. The global setting is `review-mode.json` in `KOARD_CONFIG_DIR` when set, which must be absolute, otherwise in the `koard` directory under the operating system's user configuration directory.
7. S7 — A setting file that cannot be read, has unknown fields, an unsupported schema version, an unknown mode, or trailing data fails the command with exit code `1` and names the file. It is never read as `on`.
8. S8 — Usage errors (missing or unknown subcommand, unknown scope, flags on `status`) exit with code `2`.
9. S9 — Nothing consumes the mode yet. Documentation must say so instead of implying that reviews run.
10. S10 — Public-interface tests for every observable above; update help, `README.md`, `docs/review-mode.md`, the docs index, `docs/architecture.md`, and the roadmap; run `go test ./...` and `go vet ./...`.

## Tasks

- [x] T1 — S1-S10 — route: inline (one behavior, no writer trigger) — risk: none — commit: pending (the user commits by hand).

## Log

- L1 (2026-11-15): "Listo, sigamos con el siguiente paso del roadmap".
- L2 (2026-11-15): Step 1 merged into `develop` as `90a6810`. `koard.yaml` cannot carry the mode without schema version 2, and it is committed, so a repository change could flip a user-owned switch. `.koard/state/` is ignored by git, which makes it the clone-local location.
- L3 (2026-11-15): RED observed: nine of ten new CLI tests failed because `review` was an unknown command. `TestReviewModeUsageErrors` already passed for the same reason, so it pins exit code `2` instead of proving the new parser.
- L4 (2026-11-15): GREEN with `internal/core/reviewmode` (resolution, strict setting files, atomic writes) and `internal/cli/review.go`. `go test ./...`, `go vet ./...`, and `gofmt -l internal cmd` clean.
- L5 (2026-11-15): Guard check in a scratch copy: moving the global-off case after clone-on made `TestReviewModeGlobalOffWinsOverCloneOn` fail, so the off-wins rule is pinned. A built binary run against a scratch project and `KOARD_CONFIG_DIR` printed the documented output and wrote `{"schema_version":1,"mode":"off"}`.
- L6 (2026-11-15): `docs/architecture.md` still listed cancellation as later work after task 1.6; corrected in this change.
