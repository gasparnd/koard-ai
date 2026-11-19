# Understand, debug, and extend runtime adapters

An adapter translates **Koard's contracts into a runtime invocation and back**.
The contract belongs to Koard, not to the repository being inspected. OpenCode
and Codex use different commands, output channels, and safety mechanisms; an
adapter keeps those differences outside the orchestration core.

## Follow one request

```text
CLI selects the configured provider for the role
  → core supplies a typed request and capabilities
  → adapter validates input and runtime version
  → adapter explains the result contract and invokes the runtime read-only
  → adapter extracts the final answer from the runtime's output channel
  → strict decoding and core-owned validation admit or reject the result
  → orchestrator records evidence and decides the next step
```

`agentrouter.Runner.Execute` handles workflow handoffs. Both current adapters
delegate it to `Propose`. `review.Reviewer.Review` handles independent review:
the adapter materializes the supplied immutable snapshot with
`snapshot.Materialize`, runs against those bytes, and cleans up the temporary
tree. The review core validates result identity, target, attempt, and findings.

| Responsibility | Owner |
|---|---|
| Handoff/result types and binding rules | `internal/core/handoff` |
| Routing, workflow persistence and transitions | Core/application services |
| Consent, review lifecycle, receipts and apply | Their respective core packages |
| Command flags, version support, prompts and output extraction | Runtime adapter |
| Provider selection and composition | `internal/cli` |
| Authentication | Runtime's existing credential mechanism, not committed Koard config |

Adapters matter because changing runtimes must not change who grants authority,
what counts as evidence, or which result belongs to a request. A model's `next`
field is advice, not permission. A successful process is not necessarily a valid
result; a valid `blocked` result is not completed work. Even a completed planner
does not prove a complete workflow, review receipt, or application.

## Separate the two contracts

The **transport** is runtime-specific: events, progress output, and final-message
files. The **result** is Koard's provider-neutral JSON object. Do not decode a
progress stream as if it were already a result.

| Runtime | Result extraction | Read-only mechanism |
|---|---|---|
| OpenCode | `--format json` NDJSON; collect text suffixes and select the completed final step | Inline deny-by-default tool policy with repository inspection tools allowed |
| Codex | Read the `--output-last-message` file, never progress stdout | `-s read-only` sandbox with ephemeral execution and user-config/rules suppression |

These are not equivalent isolation mechanisms. Ambient/project configuration
limitations remain: consult [architecture](architecture.md#runtime-adapters)
and [Codex limits](codex-adapter.md#limits-stated-plainly). Neither adapter claims
complete network isolation or that all externally configured processes are confined.

Prompts must explain the **result**, not merely repeat a request's schema URI.
Handoffs require `schema_version`, `handoff_id`, `role`, `status`, and `summary`.
Review results instead require `schema_version`, `request_id`, `attempt`, `target`,
and `verdict`. Their nested evidence shapes also differ: handoff references have
`kind`, `uri`, and optional `digest`; review findings use `uri` and `digest`.
See [handoffs](handoffs.md) and [independent review](independent-review.md).

Codex's prompts enumerate required and optional fields, binding instructions,
allowed statuses/verdicts, and the prohibition on extra fields. This helps the
model produce the existing contract; it does **not** replace strict validation,
repair malformed output, or guarantee model compliance.

## Debug from the first failing stage

Start with persisted `workflow.json`, `events.jsonl`, and `handoffs/` under
`.koard/state/runs/<id>/`. For guided workflows, `koard odd status --workflow <id>`
is read-only. Use `koard review status --workflow <id>` for review state.

| Stage / symptom | Inspect next | Do not do |
|---|---|---|
| Provider unresolved or unsupported capability | Role configuration and requested capabilities | Silently choose another provider or relax the guard |
| Version check refused | Executable/version and the adapter's supported flag surface | Remove the version gate to force execution |
| Process/API failure, quota or timeout | Exit status and sanitized provider error category | Treat it as malformed result JSON or retry automatically |
| Missing final file / incomplete event stream | Runtime output-channel framing | Fall back to progress text or an earlier answer |
| JSON decode failure / unknown field | Result field definitions and exact harmless field name | Ignore unknown fields, strip fences, or repair JSON |
| Identity, role, target, attempt or digest mismatch | Original request and bound snapshot evidence | Rewrite identifiers to make the response pass |
| Consent or stale receipt refusal | Exact candidate and lifecycle state | Treat a model verdict as authorization |

Capture only what is needed: runtime version, model, stage, elapsed time, exit
status, event types, final-answer presence, and sanitized error category. Keep
harmless contract field names when possible; redacting every quoted value can
erase the useful diagnosis. Never publish credentials, raw argv/prompts, source
content, tool bodies, or unfiltered provider stderr. Current adapter errors can
include subprocess stderr: sanitize before sharing. Usage is unknown unless
actually reported; partial step counters are not total cost.

Reproduce with a fake runner or executable fixture first. A live probe requires
explicit authorization for the provider/session and content sent. Use a minimal
isolated fixture, one bounded invocation, and no downstream execution. A fresh
workflow ID is needed for another routed run; do not overwrite failed evidence.

## Add another runtime adapter

1. Create `internal/adapters/<runtime>` with configuration and an injectable
   command boundary. Import core contracts; never import the adapter from core.
2. Implement `agentrouter.Runner.Execute(context.Context, handoff.Request)` and,
   if supported, `review.Reviewer.Review(context.Context, review.Request, string)`.
   Add compile-time interface assertions like the existing adapters' tests.
3. Validate requests and reuse `mutation.ValidateProposalCapabilities`. Establish
   a verified version gate, fail-closed read-only invocation, cancellation, and
   temporary-output cleanup. Unsupported review must fail explicitly.
4. Explain the existing result fields and bindings to the model. Extract exactly
   one final answer using the runtime's documented channel. Reuse handoff decoding
   and `ValidateResultFor`; keep semantic review validation in the review core.
   Reuse `snapshot.Materialize` for review; never substitute the live workspace.
5. Wire workflow construction **and** the supported-provider check in
   `internal/cli/run.go`. Separately wire reviewer/corrector construction and
   application-owned reviewer identity in `internal/cli/review_run.go`.
   Configuration already carries provider/model per role; do not invent a new
   schema unless the integration actually requires one.
6. Add deterministic public-interface tests: valid bound results, all used roles,
   malformed/extra/trailing JSON, wrong bindings, process failure/cancellation,
   required/forbidden flags, missing final output, and snapshot cleanup. Add CLI
   composition coverage and confirm existing providers still behave unchanged.
7. Document supported versions, authentication prerequisites, output framing,
   safety limits, configuration and troubleshooting. Keep unimplemented support
   marked as planned. Run the checks below before any separately authorized live
   smoke test.

```bash
go test ./internal/adapters/... ./internal/cli
go test ./...
go vet ./...
```

Start from [Codex's adapter](codex-adapter.md) for a file-based final answer or
`internal/adapters/opencode/transport.go` for event framing. Copy the boundary
design, not another runtime's flags or safety assumptions. Engram is a separate
service adapter implementing memory responsibilities; see [memory](memory.md).
