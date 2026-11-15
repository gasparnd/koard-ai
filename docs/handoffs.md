# Provider-neutral handoffs

Koard coordinates roles through versioned JSON artifacts. Providers translate these contracts to their own execution APIs, but provider names, SDK objects, and conversational state never enter the core protocol.

The current contract is internal to Koard. It is versioned and tested, but it is not yet a published third-party API.

## Flow

```text
orchestrator creates Request
  → assigned role performs bounded work
  → role returns Result
  → orchestrator validates identity and role
  → orchestrator decides whether to create another Request
```

Agents cannot authorize delivery or another agent merely by returning a `next` value.

## Roles

Version `1` recognizes three roles:

| Role | Responsibility |
|---|---|
| `planner` | Produce a bounded plan or decision artifact |
| `executor` | Implement the authorized objective and return evidence |
| `verifier` | Independently evaluate the candidate and evidence |

## Request contract

```json
{
  "schema_version": 1,
  "id": "handoff-1",
  "task_id": "task-1",
  "from": "planner",
  "to": "executor",
  "objective": "Implement provider-neutral contracts",
  "context": [
    {
      "kind": "repository",
      "uri": "koard://repository/current",
      "digest": "sha256:candidate"
    }
  ],
  "capabilities": ["repository:read"],
  "expected_output": {
    "format": "application/json",
    "schema": "koard://schemas/handoff-result/v1"
  }
}
```

A request requires unique handoff and task identities, distinct valid source and destination roles, an objective, and an output format. Context references and capabilities are explicit rather than inferred from ambient access.

## Result contract

```json
{
  "schema_version": 1,
  "handoff_id": "handoff-1",
  "role": "executor",
  "status": "completed",
  "summary": "Contracts implemented and verified.",
  "artifacts": [
    {
      "kind": "source",
      "uri": "repo://internal/core/handoff/handoff.go",
      "digest": "sha256:artifact"
    }
  ],
  "evidence": [
    {
      "kind": "test",
      "uri": "command://go-test",
      "digest": "sha256:evidence"
    }
  ],
  "next": {
    "role": "verifier",
    "reason": "Verify the candidate."
  }
}
```

Result status is one of `completed`, `blocked`, or `failed`. The result must reference the exact request ID and must come from the request's destination role. Artifacts and evidence use explicit references so future immutable candidate and RDD contracts can bind their digests.

A completed executor result is what `internal/core/candidate` turns into a candidate: the identity is the SHA-256 digest of the result summary plus its artifacts and evidence, so the references declared here are the content the identity binds.

## Validation guarantees

The implementation in `internal/core/handoff`:

- rejects unsupported schema versions;
- rejects unknown JSON fields and multiple JSON values;
- validates roles, statuses, required values, references, and transitions;
- binds each result to its request identity and destination role;
- emits deterministic indented JSON from typed Go values.

## Runtime adapter boundary

The proposal-only OpenCode adapter implements this protocol without changing its semantics. It validates a request before invocation, permits only the `repository:read` capability, requires a JSON result, and applies the same strict decoding and identity/role binding described above.

The agent router composes this protocol into a fixed route: the planner hands off to the executor, then the executor hands off to the verifier. Each hop is an independent request/result pair with its own identity, so a result can only answer the handoff it was created for.

The second request carries what it must verify: a `Context` reference with kind `koard.handoff.result`, a state-relative URI, and a `sha256:` digest of the persisted result bytes, followed by any artifact the responder declared. The verifier therefore inspects the exact proposal instead of a paraphrase of it.

The adapter does not authorize the next transition returned by a model. Agent routing records the route, and later workflow phases decide transitions.
