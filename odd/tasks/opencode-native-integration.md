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

- T1 [S1,S3] Native setup with public-interface tests and documentation — route: inline; status: setup and native activation verified; commit: `ea21596`. Risk: item 5 (installer/configuration behavior), item 2 (filesystem confinement).
- T2 [S1,S2] Native orchestrator and bounded role/skill assets with tests and usage documentation — route: inline; status: native skill loading, delegation and fixture implementation verified; effective denial probe pending T3; commit: `ea21596`. Risk: item 3 (consumed instructions), item 2 (authority boundaries).
- T3 [S1,S2,S3] Verify Engram host-binding integration and native end-to-end behavior; implement only evidence-supported wiring — route: inline, native probe execution delegated by the per-action rule; status: partial, blocked after bounded host timeout; commit: pending. Risk: item 2 (session attribution), item 5 (runtime integration). Missing native proof must remain explicit.

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

### L6 — First implementation and checks
Tracker commit: `e8d2a9e`. Implemented `koard native setup --runtime opencode [--dry-run]`, complete preflight, no-replace publication, identical-byte retry, three native agent definitions and the bundled ODD skill/guide. Existing commands and execution adapters remain unchanged except additive dispatch/help. T1 and T2 form one coherent setup delivery; splitting assets from their installer would produce an unusable command.

Observed RED: after correcting a test syntax typo, `go test ./internal/cli -run TestNativeSetup -count=1` failed on unknown `native` command. Observed GREEN: focused tests passed after implementation. One preservation test initially assumed `review --help` exited 0; actual pre-existing behavior exits 2, and the assertion was corrected without changing production behavior.

Command worker final results: `go test ./...`, `go vet ./...`, source strict skill validation and installed strict skill validation passed. Initial isolated dependency-cache and harness-path errors were corrected. All 18 differential command cases (`--help` and `--dry-run` for nine existing commands) matched baseline stdout, stderr and exit codes exactly. Parent repeated source strict skill validation successfully.

Independent verifier ran 26 isolated CLI invocations, including preview, installation, retry, argument failures and eight conflict fixtures. Rejected requests preserved file hashes, directories and symlink targets. No installer blocker reproduced. Returned a dangling-skill-symlink regression probe, added to the suite. S2 is supported at instruction-contract level; S1/S3 remain unmet until actual host activation, effective permissions, delegation and Engram continuity are verified. No host/model session was launched or global configuration changed. Native review mode is globally on; assessment remains pending after normalization and final checks.

### L7 — Implementation commit and review assessment
Commit `ea21596` contains the coherent setup behavior, assets, tests and docs (726 authored added/deleted lines, including tracking updates). The user selected one feature branch; no PR or publishing occurred. Final focused checks including the verifier probe and `git diff --check` passed. Native assessment against `e8d2a9e` returned `high`, `review_due: true`, reason `high_risk`, with item 5 escalation. Candidate-specific review consent is the next native transition; no review approval is claimed. T3 still needs explicitly scoped host-session proof and a working authoritative Engram integration.

### L8 — Review closure and remaining proof
Verification-log commit: `c296f37`. The user granted candidate-specific native review. All four lenses completed; review approved the committed candidate through `c296f37`. Exact acknowledgement succeeded for lineage `review-d9d2ac4450c3a69e`, target `sha256:41048d432578099780f216161d6be7f3de1292dff6942c4b37339df4a7365ce5`; terminal authority was burned. Non-blocking advisory R3-1: the late-conflict test exercises preflight, not a failure after publication; deterministic partial-publication/retry coverage is follow-up work, not a correction of this reviewed candidate. Both verification scratch directories were removed (read-only Go cache directories required owner permission restoration before cleanup). No native host/model smoke or Engram continuity proof has run. Next decision: authorize a bounded real OpenCode smoke in an isolated project, with explicit host authentication/model use and isolated memory storage; no global configuration changes or production memory writes.

### L9 — Native smoke authorization
"vamos con el siguiente paso"

The user accepted the immediately preceding proposal: run a real OpenCode smoke in a temporary project, using OpenCode's configured model and authentication, with isolated memory and no global configuration changes. This supersedes L2's prohibition on model invocations only within that bounded smoke scope. Parent observed OpenCode v2.2.6 and its native `run` flags. The source worktree was clean at `568455c`; remaining work stays on the same feature branch. Delegate the long native probe as command execution, not implementation. Before starting a host process, establish memory storage isolation; if unavailable, stop that probe rather than touching production memory. No authentication copying, permission bypass, production memory writes or global setup is authorized.

### L10 — Native smoke results and remaining decision
The initial preflight stopped before launch because the parent's scratch-only requirement also isolated OpenCode's saved authentication database. That constraint exceeded the user's temporary-project/isolated-memory scope; the corrected probe allowed normal native OpenCode session state, with no credential copying or global configuration changes.

OpenCode v2.2.6 selected `koard` and `opencode/tidepool-alpha-free` without forced agent/model options. The installed skill path and completed `koard-explorer` subagent were confirmed through structured events. The primary implemented a Python status-filter fixture, observed RED (five tests, three errors), then GREEN (five passing tests). Both the worker and parent independently reran `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v`: five tests passed. Source assets and checked global config hashes were unchanged. The repository only contains tracking/documentation changes from this probe.

The isolated Engram store contains one authoritative root-attributed session and one synthetic prompt, no child session, and zero observations. The root turn reached the 420-second harness limit and was terminated (host exit 143, outcome interrupted) before memory save/recall. The child reported write capability unavailable and no canary appeared, but there was no actual attempted-write denial. These are partial results, not end-to-end approval. All owned host/Engram process groups stopped and isolated ports closed. External scratch remains retained for the unresolved continuation; its locator is in the session handoff.

Remaining decision: authorize one additional bounded continuation for the missing memory save/recall and permission evidence, or accept these as deferred proof. No automatic repeated model attempts or model switch was performed. Automatic Engram installation is still not implemented.
