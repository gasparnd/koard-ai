# Candidate-bound review consent

Review consent records whether the user permits independent review of one exact candidate snapshot. It does not authorize correction, workspace mutation, commit, push, pull request, merge, release, or any other delivery action.

## Commands

```bash
koard review consent status --workflow <id>
koard review consent grant --workflow <id>
koard review consent decline --workflow <id>
```

The workflow identity selects `.koard/state/runs/<id>/workflow.json` in the current Koard project. Koard loads the completed workflow, verifies its candidate, risk assessment, snapshot manifest, and stored blobs, then reports the exact target:

```text
Review consent: granted
  workflow: workflow-1
  candidate: sha256:<candidate digest>
  snapshot: sha256:<snapshot digest>
  risk: medium
  authority: independent review of this snapshot only
  mutation and delivery: not authorized
```

`status` only reads and reports `unset`, `granted`, or `declined`. Grant and decline require the effective [review mode](review-mode.md) to be on.

Review-mode writes and consent decisions are serialized through a lock in the global Koard configuration directory. Concurrent operations fail closed; this prevents review mode from being disabled between the consent check and publication. If a process terminates while holding the lock, remove `.review-mutation.lock` only after confirming no Koard review operation is still running.

## Binding and persistence

The run directory stores one versioned `consent.json` record containing:

- workflow, candidate, and snapshot identities;
- the candidate risk tier;
- the `granted` or `declined` decision;
- the exact authority `review`.

The file is published atomically and never replaced. Repeating the same decision for the same target is idempotent. A conflicting decision, changed target, invalid workflow record, mismatched risk assessment, or unverifiable snapshot fails without changing the stored decision.

A decline is scoped to that candidate snapshot and does not disable review mode. Changing a recorded decision requires a new workflow and candidate.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Status was read or the decision was recorded idempotently |
| `1` | Review mode is off, workflow state is missing or invalid, the snapshot cannot be verified, or a conflicting decision already exists |
| `2` | Usage error: missing or unknown action, missing workflow identity, invalid workflow identity, or unexpected arguments |
