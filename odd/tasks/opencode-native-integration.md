# Native OpenCode integration
Objective: make native conversation the primary Koard experience, with hybrid instruction-led orchestration and deterministic operations.
Baseline: `d571a56`; branch: `feat/opencode-native-integration`. Delivery: `single-pr` (one feature branch, incremental commits; publishing remains unauthorized); forecast 800–1,200 authored changed lines (estimate, not a limit). Checks: public CLI tests, `go test ./...`, `go vet ./...`, independent verification, separately scoped native smoke.

## Specs

### S1 — Native host experience
User requirement: "Yo no quiero utizar koard para todo, quiero instalar koard  con el agente orquestador, los arneses, skills y tools pero utiliza cualquier cli (opencode, copilot, codex, claude) y que tome el orquestador y este derive en base a olas reglas, skills, odd y engram. No quiero usar koard odd run siemre. Quiero que funcione como genlte-ai"

Authorized first delivery targets OpenCode; the other runtimes remain later work. Acceptance: a fresh native session can discover the Koard orchestrator and instructions without manually starting `koard odd run`. Existing execution adapters remain supported. Native activation requires real runtime evidence, not asset string assertions alone.

### S2 — Hybrid governance
User decision: "vamos con esa combinacion"

Accepted proposal combines orchestrator instructions and skills with deterministic tools for sensitive operations, state and consent; it does not route every edit through a state machine. Acceptance: the native instructions distinguish ordinary host edits from existing snapshot-bound Koard review/apply. No claim that a receipt governs unrelated chat edits. User permissions and publishing decisions remain separate.

### S3 — Implement the first integration
User approval: "si"

Approval answers: "¿Autorizás comenzar la implementación por la integración con OpenCode, como primera etapa de la propuesta?"

Implementation approach: project-local, explicit setup with preview, non-overwrite behavior and convergent retries; no installation into the user's global configuration. OpenCode capability/version support must be verified. Memory continuity requires authoritative host session binding, never a generated substitute session ID. Existing working host Engram integration may be reused; automatic lifecycle setup remains unresolved until its supported contract is verified.

## Tasks

- T1 [S1,S3] Native setup with public-interface tests and documentation — route: inline; status: in progress; commit: pending. Risk: item 5 (installer/configuration behavior), item 2 (filesystem confinement).
- T2 [S1,S2] Native orchestrator and bounded role/skill assets with tests and usage documentation — route: inline; status: pending T1; commit: pending. Risk: item 3 (consumed instructions), item 2 (authority boundaries).
- T3 [S1,S2,S3] Verify Engram host-binding integration and native end-to-end behavior; implement only evidence-supported wiring — route: inline, independent verifier for named risks; status: pending; commit: pending. Risk: item 2 (session attribution), item 5 (runtime integration). Missing native proof must remain explicit.

## Log

### L1 — Original request
"okay, creo que estamos haciendo algo mal. Yo no quiero utizar koard para todo, quiero instalar koard  con el agente orquestador, los arneses, skills y tools pero utiliza cualquier cli (opencode, copilot, codex, claude) y que tome el orquestador y este derive en base a olas reglas, skills, odd y engram. No quiero usar koard odd run siemre. Quiero que funcione como genlte-ai"

### L2 — Authorization, 2026-11-22
User selected the hybrid proposal: "vamos con esa combinacion". User then authorized first-stage OpenCode implementation: "si". This does not authorize publishing, global configuration changes, or paid native model invocations.

### L3 — Mapping and baseline
Worktree was clean on `feat/claude-adapter` at `d571a56`. Read-only delegated mapping was required to validate native host capabilities and decide integration seams; no writer delegation reason exists, so implementation stays inline.

Existing `sync` only projects declared skills and leaves native settings untouched (`docs/sync.md:140–156`). Runtime-executing adapters intentionally differ from native interactive integration. Existing Engram adapter owns a per-run child/session and must not be reused as host-session ownership without evidence.

Official OpenCode pages inspected by the mapping worker: `/docs/agents`, `/docs/config`, `/docs/tools`, `/docs/mcp-servers`, `/docs/build/plugins/server` at `https://opencode.ai`. They document project agent Markdown, project JSONC configuration and `default_agent`; existing sessions retain their agent. Runtime activation and root-session Engram binding remain unverified.

### L4 — Delivery and verification plan
Forecast exceeds approximately 400 authored lines; choose a chain strategy before the first work-unit commit. No push, PR creation or merge is authorized. Preserve tests/docs with behavior, not separate commits by file type. Use observed RED → GREEN for deterministic setup behavior. Run normalizers before tests and any native review freeze. An independent verifier is required for installer and authority changes. Native model smoke needs explicit execution scope; do not call an unperformed smoke test passed.

### L5 — Delivery decision, 2026-11-22
"manten todo en una rama pero haz commit. Cuando cambiemos de tarea avisame y crea una nueva rama"

Keep this entire OpenCode integration in `feat/opencode-native-integration` with incremental commits. Notify the user and create a new branch when moving to a different feature/task, not for internal steps T1–T3 of this same integration. The branch starts at the current Claude adapter baseline without rewriting it. Source implementation is still pending; this initial commit records the approved scope and delivery decision only. The earlier chain-choice gate is resolved by the user's single-branch instruction.
