---
name: koard-odd
description: "Trigger: native Koard development request. Apply proportionate ODD inside the host conversation."
license: Apache-2.0
metadata:
  author: koard-project
  version: "1.0"
---

## Activation Contract

Load for each native Koard request. Inputs are the user's objective, project
instructions and current evidence. This skill does not grant permission.

## Hard Rules

- Keep investigation and proposal requests read-only until implementation is authorized.
- Preserve user changes and host permission boundaries. Ask one focused question
  when a consequential product decision is unresolved, then wait.
- Native chat edits are not covered by Koard snapshot receipts.
- Do not commit, publish or perform destructive actions without authorization.

## Decision Gates

| Condition | Action |
| --- | --- |
| Understood, contained task | Implement inline without durable task ceremony |
| Work cannot be resumed from request and diff | Track requirements, progress and proof in a project task document |
| Broad mapping needed | Delegate a bounded read-only question |
| Security, data loss, concurrency or installer risk | Request independent verification after own checks |
| Missing authority or required proof | Report the exact blocker, never invent approval |

## Execution Steps

1. Establish authorization, inspect project instructions and relevant code.
2. Load matching skills; recall verified project context when Engram is available.
3. Resolve uncertainty and choose the smallest safe implementation route.
4. For runnable deterministic behavior, observe a failing regression test before
   the fix, implement the fix, then refactor while checks remain green.
5. Run focused tests and applicable project checks; preserve observed results.
6. Update documentation with changed behavior. Keep tracked progress recoverable;
   save significant decisions through the authoritative host memory integration.
7. Report the outcome, risks, failed or skipped checks and next action. Continue
   already-authorized work unless blocked; do not ask to continue every step.

## Output Contract

Return concise user-facing results and explicit proof limits. Delegated reports
contain scope, evidence and unresolved questions, not new authority.

## References

- [Native ODD guide](README.md)
