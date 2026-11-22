# Run a read-only workflow

`koard run` connects the provider-neutral router to the runtime configured for each executed role. Choose a bounded policy; the model cannot add steps or authorize changes.

## Quick path

1. Run `koard init` and configure `agents.executor` and `agents.verifier` in `.koard/koard.yaml`; configure `agents.planner` too for `planned`.
2. Set each required provider to `opencode`, `codex` or `claude` and its model to a real model identifier. Each runtime must already be installed and authenticated. Construction checks configuration; runtime/version failures are recorded when execution starts. Claude is tool-free and requires prepared context rather than exploring files; start with the [mixed-runtime example](claude-adapter.md#quick-path).
3. Run a new workflow identity:

```bash
koard run --workflow example-1 --task task-1 --objective "Propose a documentation improvement" --policy planned
```

| Policy | Invoked roles | Default |
|---|---|---|
| `fixed` | executor → verifier | Yes; preserves the original route |
| `planned` | planner → executor → verifier | No; explicitly selected |

`fixed` starts with a planner-to-executor handoff but does not invoke a planner. `planned` first sends an orchestrator-to-planner request, then hands its persisted plan to the executor. The orchestrator is a sender, not another model role. Policies are closed, sequential, and finite; arbitrary routes, retries, conditional routing, and model-selected transitions are not supported.

`workflow.default` still names the broader workflow contract (`odd` at initialization), not a routing policy. This command does not execute the ODD state machine. No configuration schema change is needed. Structural validation still accepts `unset`; execution rejects unresolved providers/models for required roles before creating run state. The unused planner configuration may remain `unset` with `fixed`.

## Evidence and exit status

Each run claims `.koard/state/runs/<workflow>/` once, with its existing `workflow.json`, `handoffs/`, `events.jsonl`, and immutable executor snapshot. A reused identity fails; use a new identity for a retry. New outcomes optionally record `policy`; an older outcome without it describes the original fixed route.

Every subsequent request carries the previous result's URI and digest, followed by its declared artifacts. Persisted result references are run-relative (`handoffs/1-result.json`); CLI composition resolves them to `repo://.koard/state/runs/<workflow>/handoffs/...` for the adapter's repository view, without changing the result bytes or digest. The executor receives the plan as typed evidence, not a conversational summary. Only the executor creates the candidate; a blocked or failed planner creates none.

The command prints workflow, policy, and status, plus candidate/snapshot identities when present. Exit `0` means the route completed, `1` means configuration/execution failed or a role reported `blocked`/`failed`, and `2` means invalid command syntax or missing required arguments. Errors go to stderr. A claimed run retains its terminal record, including provider failures and handled interrupts. `koard run --help` shows command syntax without invoking a provider.

## Authority and current limits

All provider calls remain proposal-only with `repository:read`. A route's verifier result is **not** an independent-review receipt. Run the separate `koard review consent` and `koard review run` lifecycle when appropriate; this command grants neither consent nor correction authority, never applies files, and never performs delivery.

Snapshots still capture existing `repo://` artifact bytes, not arbitrary new content suggested in a model's prose. This routing slice does not add a patch-writing protocol. Provider/model identities are selected from configuration but are not yet persisted as per-handoff execution provenance. Real multi-runtime orchestration with receipts remains a separate roadmap milestone; this slice's deterministic integration tests substitute providers.
