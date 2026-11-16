# Router policies and CLI execution

Objective: preserve the fixed route and add real planning through a provider-neutral policy selected by CLI. Branch: `task/3.2-router-policies`; baseline: `542ccf3`.
Delivery: `feature-branch-chain`, approved by the user; tracker `task/3.2-router-policies`, then `task/3.2-router-core`, then `task/3.2-router-cli`. No publishing. Forecast about 550 authored changed lines; observed 666 before final log updates. Runner: `go test` and `go vet`. Route: inline; dependent core/CLI work, no parallel writer trigger.

## Specs

1. S1 — "Generalized Agent Router policies beyond the fixed route", including "CLI wiring del router". Expose deterministic named policies, not model-directed transitions.
2. S2 — Approved proposal: "conservar la ruta actual y agregar una ruta con **planificación real → ejecución → verificación**, seleccionable desde el CLI, con proveedor por rol y sin ampliar permisos ni automatizar cambios."
3. S3 — "dale, sigamos segun lo planeado como objetivo de koard ai": preserve provider-neutral core, typed handoffs, persisted evidence, and separate human-controlled review/apply/delivery. No retries, resume, memory adapter, or ODD state-machine integration in this slice.

## Tasks

- [x] T1 — S1-S3 — inline — implementation verified — named core policies, explicit sender, legacy outcome compatibility and tests; RED missing Options.Policy, GREEN focused tests. Commit pending chain decision. Risk: item 3 (handoff sender contract and shared router).
- [x] T2 — S1-S3 — inline — implementation verified — CLI execution with per-role providers, tests and synchronized documentation; RED missing workflowProvider, GREEN public CLI tests. Commit pending chain decision. Risk: item 2 (composition must preserve read-only provider execution).
- [ ] T3 — S1-S3 — inline plus independent verifier — in progress — suite/vet, differential checks and independent verification passed; native review and commits pending. Risk: items 2 and 3.

## Log

- L1 (2026-11-16): User: `roadmap Orchestration expansion → "Generalized Agent Router policies beyond the fixed route", que además es lo que destraba el CLI wiring del router (sigue sin existir desde el walking skeleton).`
- L2 (2026-11-16): User: `dale, sigamos segun lo planeado como objetivo de koard ai`.
- L3 (2026-11-16): Proposed: `Propongo conservar la ruta actual y agregar una ruta con **planificación real → ejecución → verificación**, seleccionable desde el CLI, con proveedor por rol y sin ampliar permisos ni automatizar cambios.` User: `dale`.
- L4: Mapping confirmed no previous policy catalog. Existing route invokes executor and verifier only. The handoff contract rejects equal sender/destination, so real planning needs an explicit orchestrator sender, not a fabricated verifier/executor origin. Keep the orchestrator sender-only. Preserve old requests and outcomes; policy metadata is optional for old records.
- L5: Implementation choices: `fixed` remains the default; `planned` adds orchestrator → planner before the existing hops. CLI `koard run --workflow <id> --task <id> --objective <text> [--policy fixed|planned]`. `workflow.default: odd` retains its existing meaning and is not reinterpreted as a policy selector. Tests use injected runners, never paid provider calls.
- L6: T1/T2 implemented inline. CLI composition resolves the generated prior-result reference from its run-relative URI into a repository URI; declared artifact references remain unchanged. Both policies retain read-only capabilities and independent review/apply boundaries.
- L7: Observed `go test ./...` and `go vet ./...` passing. Baseline/current differential checks of nine existing command forms with `--policy planned` preserved stdout, stderr and exit codes; help intentionally adds `run`. No real model providers were invoked.
- L8: Independent verifier reported S1-S3 met and no reproduced blockers. Its default outcome probe matched baseline bytes; its eight CLI cases pinned role-specific models, context digests, terminal states, executor ownership and replay immutability. Sender-boundary and default-outcome probes were added as tests; model-role and replay assertions were integrated into CLI tests. Parent spot check: `go test ./internal/cli ./internal/core/agentrouter ./internal/core/handoff -count=1` passed; `git diff --check` passed.
- L9: Measured 666 authored changed lines including these log updates. Proposed cohesive work units: core policies/contract plus tests/docs, then CLI composition plus tests/docs. Chain strategy must be chosen before the first commit. No commits or publishing yet. Native review mode is on. Initial assessment refused untracked files; staging the 18 intended files resolved that prerequisite. Reassessment returned high, review_due=true, high_risk (explicit item 3 escalation). Native review/consent remains pending; no START has occurred. Temporary differential/verifier directories were removed after incorporating probes.
- L10 (2026-11-16): Asked: `El cambio suma unas 666 líneas. ¿Lo organizamos en una **cadena sobre rama feature**, separando core y CLI, sin publicar nada todavía?` User: `dale`. Chain approved; no remote operation authorized. Core commit includes only core code/tests and handoff documentation; CLI docs and composition follow in the dependent child. Each unit receives committed-only native assessment against its immediate base.
