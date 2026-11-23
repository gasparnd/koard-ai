# Project-local Engram setup
Objective: configure Engram's native OpenCode integration without global setup or manual plugin/MCP wiring.
Branch: `feat/opencode-engram-setup`; baseline: `1452aac`. Delivery: single-pr strategy on one branch; no publishing. Forecast: about 500 authored lines plus unchanged third-party assets, kept together as one installer behavior with its tests/docs. Checks: public CLI RED/GREEN, bundle integrity, compatibility differential, Go suite/vet and independent verification.

## Specs

### S1 — Automatic native memory configuration
User: "vamos con eso" accepts the preceding recommendation: "El próximo trabajo debería ser ese, no otra ronda de pruebas aisladas."

The referenced task is "Configurar Engram desde el instalador, sin depender de una integración previa." Implement an explicit project-local opt-in for the existing OpenCode setup, using the provider-owned plugin and MCP configuration. Require an already installed compatible Engram executable; no binary installation, global setup, model invocation or production-memory operation is authorized. Configuration success is not host activation or verified continuity. Existing configuration migration remains separate work.

### S2 — Preserve user configuration and delivery
Inherited instruction: "manten todo en una rama pero haz commit. Cuando cambiemos de tarea avisame y crea una nueva rama"

The task and new branch were announced. Preserve current assets-only setup behavior, preview, conflict refusal, symlink protection and identical-file idempotency. Never overwrite existing native configuration or copy ambient global plugin source. Publish no remote changes.

## Tasks

- T1 [S1,S2] Add optional native Engram bundle, dependency preflight, public-interface tests and documentation — route: inline (no writer trigger); status: verified for configuration; commit: this work unit. Risk: item 2 (host session attribution integration), item 5 (third-party executable asset and setup).
- T2 [S1,S2] Verify compatibility/preservation, isolated checks and independent assessment; record delivery — route: inline with independent verifier for items 2/5; status: configuration checks passed, optional activation proof unavailable; commit: this work unit.

## Log

### L1 — Authorization and mapping
"vamos con eso"

Existing setup is assets-only. Read-only mapping was delegated because provider delivery and native binding ownership were unknown. Engram 3.7.2 setup has no local/export/dry-run contract and modifies global configuration; unknown options can invoke its interactive installer. Do not invoke it from Koard.

### L2 — Integration design
Vendor the unchanged official MIT-licensed OpenCode plugin from Engram release 3.7.2, commit `c6ba9e3c0e98ffa0d5591481d8670a575e7b70b0`, plugin Git blob `b1d40a3b96e0b939578dc784f8f38effa6d57144`. This is the same artifact embedded by provider setup. No separate OpenCode package is published. Compose the optional bundle at the adapter boundary and reuse `nativeintegration.Setup`; do not change core, proposal adapters or publication rules. Installation is offline; this development-time source import is pinned and licensed.

OpenCode v2.2.6 activates the first duplicate plugin ID; a global Engram plugin can win over the local copy. Setup cannot certify ambient runtime activation. Documentation/output must expose this limitation and prohibit claiming continuity from installed files alone. Both plugin and MCP use `engram` from the host PATH; conflicting `ENGRAM_BIN` overrides must refuse opt-in setup. Existing differing native configurations continue to refuse without writes. A later host launch can have a different environment; setup is not a sandbox or runtime readiness guarantee.

### L3 — Implementation and verification
Observed public-interface RED: `TestNativeSetupWithEngram` failed because `--with-engram` was undefined. GREEN: installation, preview, idempotency, missing/unsupported executable, override/config/plugin conflicts, symlink refusal and prior assets-only bundle preservation passed. Added verifier-derived regression probes for failed version commands, oversized stdout and timeout; they assert exit, stdout/stderr, unchanged files and exactly one `version` invocation. Parent repeated the boundary test successfully. No setup/server/model command is run by installation.

Independent candidate `go test ./...` and `go vet ./...` passed, including after the added probes. Differential checks against `1452aac` showed identical stdout/stderr/exits/file hashes for default setup and preview, and identical refusals for sibling `run`, `odd`, `review`, `memory`, `skill`, `sync` commands given `--with-engram`. Real installed Engram 3.7.2 passed isolated setup. Plugin/license digests matched the pinned source. No candidate-caused blocker was reproduced.

The optional real-host probe did not prove activation: initial harness invocations used unsupported `serve --help`/`--port`; a corrected positional-port run reached both servers, then an unnecessary GUI metadata query returned HTTP 400 before project-session creation. The plugin-list response was empty at that stage. Both owned processes were reaped. No further retry or model invocation occurred. This is unavailable runtime proof, not evidence that the installed plugin works or fails. Configuration delivery is verified; host activation/continuity remains a separate acceptance check.

Rollback boundary: remove this opt-in implementation and its third-party bundle/tests/docs together; no database format, core contract, proposal adapter or default generated asset was changed. Existing project files are never upgraded or removed automatically.

### L4 — Commit and review
Commit `fc01588` contains T1/T2's configuration implementation and checks: 509 authored changed lines plus 1,332 unchanged upstream plugin/license lines. The user granted native review. All four lenses completed; exact acknowledgement burned approved authority for `review-51465ef8c11fd9f2`. No correction was requested. The resilience lens noted non-blocking R4-1: upstream V2 event-stream interruptions can lose prompt-capture events without replay or a degraded warning. Do not modify the pinned provider source as part of this delivery or treat review approval as continuity proof.

Owned verification scratch was removed after all processes stopped. No global setup, production-memory change, model prompt, push, PR or merge occurred. Configuration is delivered; activation remains unverified after the bounded harness attempts. The next human decision is whether to authorize one corrected, model-free local activation probe or defer that remaining proof.
