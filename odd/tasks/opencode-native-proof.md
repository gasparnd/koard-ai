# Deterministic native integration proof
Objective: resolve outstanding memory retrieval and permission evidence without further remote model turns; retain explicit proof limits.
Branch: `feat/opencode-native-proof`, baseline `ee85f16`. Delivery: one branch with incremental commits, no publishing. Forecast: 300–450 authored lines (advisory). Checks: focused opt-in integration tests, `go test ./...`, `go vet ./...` and structural documentation readback.

## Specs

### S1 — Resolve pending proof as a separate task
User approval: "oay"

This accepts: "¿Querés abrir una tarea específica para resolver esos pendientes, en una nueva rama, en lugar de seguir repitiendo pruebas con el modelo?"

Inherited scope is the native memory and permission evidence left open in `odd/tasks/opencode-native-integration.md`. Verify full Engram retrieval and the actual OpenCode permission evaluator without remote model calls. Do not label direct MCP retrieval as proof of the host binding, or evaluator results as an attempted tool-write denial. Keep unavailable runtime APIs explicit. Automatic Engram installation is not implied by these proof probes and remains separate outstanding product work.

### S2 — Preserve delivery and isolation constraints
User instruction: "manten todo en una rama pero haz commit. Cuando cambiemos de tarea avisame y crea una nueva rama"

The user was notified; the new branch starts at `ee85f16` without rewriting the previous feature. Use isolated synthetic storage and bounded owned processes. Never change global configuration, read/copy credentials, invoke remote models or write production memory. Normal tests must not launch the new external proof probes without explicit opt-in. No push, PR or merge is authorized.

## Tasks

- T1 [S1,S2] Add and exercise isolated real Engram MCP full-retrieval proof, with documentation — route: inline; status: verified; commit: this work unit, `test: add deterministic native integration proof`.
- T2 [S1,S2] Add and exercise real OpenCode evaluator allow/deny controls if the installed API supports them, with documentation — route: inline, suite and independent isolation verification delegated; status: verified; commit: this work unit, `test: add deterministic native integration proof`. Risk: item 5 (external process/environment isolation).

## Log

### L1 — Request and routing
"oay" accepts the preceding concrete proposal above. Existing implementation and smoke outcomes remain in the prior feature document. A new read-only mapping worker was needed to distinguish real deterministic APIs from invented substitutes. There is no writer-delegation reason; implementation stays inline.

### L2 — Evidence and approach
Official OpenCode OpenAPI documents `agent.list`, `agent.get`, `session.create` and `session.permission.create`. The evaluator accepts action/resources and returns an effect; it does not execute a tool. The installed runtime previously returned 404 for agent inspection, so current docs alone do not establish availability. Installed `opencode serve --help` exposes `--hostname`, `--port` and `--stdio`. Installed Engram is 3.7.2; `serve --help` is not supported and returned an error without launching a server.

Engram's existing adapter can seed owned synthetic state and acknowledges a real registered session; reuse it rather than reimplementing storage/session creation. Exercise `mem_get_observation` through real MCP with a tail marker beyond search-preview length. Tests-only verification does not require manufacturing a failing production behavior; record actual external probe outcomes and fix only harness defects within one bounded correction.

### L3 — Deterministic probe evidence
The opt-in Engram MCP test passed and returned the exact 10,224-byte observation through `mem_get_observation`. It reuses the existing adapter for synthetic creation and never claims OpenCode host binding.

An exploratory command worker first used an unsupported `--standalone serve` combination, then started plain `serve` and encountered HTTP 401 because it had retained only the endpoint. The parent verified the official `/docs/cli/web` authentication contract: generated `OPENCODE_PASSWORD`, Basic auth username `opencode`. The new opt-in test uses these credentials only for its owned isolated server. It passed in 1.27 seconds: actual evaluator results were edit deny, shell deny, read allow for both shipped inspection agents. No prompt/model was invoked; no homegrown permission matcher was used. This does not claim a real attempted-write denial. Existing shipped production assets are unchanged.

### L4 — Final verification
After normalization and the cleanup-error assertion, both focused opt-in tests passed again. Default invocation and explicit `-short` skipped both probes. An independent verifier found no candidate-caused blockers: candidate and baseline `go test ./...` passed (22 packages, four additional packages without tests); candidate `go vet ./...` passed. Empty-PATH controls skipped without opt-in and with `-short`, and failed explicitly for missing binaries when opted in. These expected negative outcomes are not failed verification prerequisites. Parent repeated the focused command successfully and `git diff --check` passed.

The independent inspection verified temporary storage, sanitized OpenCode environment, generated owned-server credentials and bounded cancellation/wait cleanup. It did not establish packet-level egress isolation or exhaustive descendant tracing. No source behavior changed, so a CLI behavior differential is inapplicable. Both requirements are verified within the documented deterministic-proof scope; full host retrieval, actual attempted-tool denial and automatic Engram setup remain separate outstanding work. Delivery remains on this branch with no publishing.
