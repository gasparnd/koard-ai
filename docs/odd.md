# ODD state machine

Koard represents Organic Driven Development as a versioned, provider-neutral state machine. This gives every runtime the same legal workflow without embedding Claude, Codex, OpenCode, or Copilot behavior in the core.

## Happy path

| Current state | Accepted event | Next state |
|---|---|---|
| `authorize` | `authorization_resolved` | `explore` |
| `explore` | `exploration_completed` | `resolve` |
| `resolve` | `resolution_completed` | `classify` |
| `classify` | `classification_completed` | `implement` |
| `implement` | `implementation_completed` | `close` |
| `close` | `closure_completed` | `completed` |

Each accepted event increments the snapshot revision. Out-of-order events fail without mutating the prior snapshot.

## Interruption behavior

Every active state accepts `blocked`. The machine records the exact active state in `resume_state` and moves to `blocked`.

From `blocked`:

- `resumed` returns to the recorded active state;
- `stopped` moves to the terminal `stopped` state;
- every other event is rejected.

Every active state also accepts `stopped` directly. Both `completed` and `stopped` are terminal and reject further transitions.

## Snapshot format

```json
{
  "schema_version": 1,
  "workflow_id": "workflow-1",
  "state": "blocked",
  "resume_state": "implement",
  "revision": 5
}
```

Decoding is strict: unknown fields, unsupported versions, invalid states, and multiple JSON values are rejected.

## Responsibility boundary

The state machine answers only: “Is this transition legal, and what is the next snapshot?”

It does not:

- decide whether business work is authorized;
- select or invoke a model;
- persist structured events;
- infer success from free-form agent text;
- commit, push, merge, release, or grant delivery authority.

The orchestrator will combine these snapshots with typed handoff results and the next roadmap capability: structured event and diagnostic output.
