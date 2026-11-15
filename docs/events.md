# Structured events and diagnostics

Koard represents machine-readable workflow output as JSON Lines. Each line is one independently decodable, versioned record associated with a workflow.

The core defines the format and validation rules. The agent router now creates and persists records for one routed workflow under `.koard/state/runs/<workflow_id>/events.jsonl`. A human-facing CLI output flag does not exist yet.

## Event record

```json
{"schema_version":1,"id":"record-1","workflow_id":"workflow-1","sequence":1,"occurred_at":"2026-11-15T12:00:00Z","kind":"event","event":{"name":"workflow.transitioned","message":"Workflow advanced from authorize to explore.","state":"explore"}}
```

Events describe observable workflow or handoff activity. The optional `state` and `handoff_id` fields bind a message to the relevant core identity without embedding provider-specific data.

## Diagnostic record

```json
{"schema_version":1,"id":"record-2","workflow_id":"workflow-1","sequence":2,"occurred_at":"2026-11-15T12:00:01Z","kind":"diagnostic","diagnostic":{"code":"KOARD_CONFIG_INVALID","severity":"error","message":"Project configuration is invalid.","detail":"schema_version 2 is not supported","hint":"Use schema_version 1."}}
```

Diagnostic severity is one of `info`, `warning`, or `error`. Stable codes let humans, agents, and automation react without parsing prose.

## Validation guarantees

Every record requires:

- schema version `1`;
- record and workflow identities;
- a sequence greater than zero;
- a non-zero timestamp;
- exactly one payload matching `kind`;
- required event or diagnostic fields;
- known diagnostic severity;
- no unknown JSON fields.

Invalid records produce no writer output. The stream decoder reads records sequentially and returns `EOF` only after the final complete record.

## Responsibility boundary

The event stream itself does not:

- create record IDs, sequences, or timestamps;
- persist or transmit records;
- select human-readable versus machine-readable CLI output;
- assign authority to an event;
- replace handoff validation or ODD transition validation.

The agent router owns record creation and persistence for a routed workflow. It emits `handoff.started`, `handoff.completed`, and a diagnostic when the run stops: `KOARD_HANDOFF_INVALID` when a result fails binding, `KOARD_WORKFLOW_CANCELLED` (warning) when the caller's context stops the run, and `KOARD_WORKFLOW_FAILED` for every other failure. A run cancelled before it starts claims no workflow identity and writes no record. Because a record carries either an event or a diagnostic payload, never both, the diagnostic detail names the handoff that was executing. The router does not yet connect ODD state transitions to this stream.
