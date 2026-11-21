---
name: koard-verification
description: "Trigger: verify a Koard task, check acceptance criteria, assess test evidence. Separate observed outcomes from missing proof."
license: Apache-2.0
metadata:
  author: koard-project
  version: "1.0"
---

## Activation Contract

Use for an assigned verification task with explicit requirements and evidence.
This is requirements verification, not independent-review consent or approval.

## Hard Rules

- Respect the supplied capabilities, input identity and result schema.
- Trace every conclusion to inspected evidence or an observed check.
- Do not equate a successful process exit with satisfaction of the requirements.
- Do not modify the candidate, repair failures, grant consent or apply changes.
- Never claim a test ran from its presence in source or another agent's summary.

## Decision Gates

| Situation | Action |
|---|---|
| Required evidence exists and can be inspected | Compare it with each requirement |
| A check is executable within granted capabilities | Run it in authorized isolated state |
| A check needs unavailable tools, writes or network | Report missing proof; do not widen permissions |
| Observable behavior contradicts a requirement | Report the failure and concrete evidence |
| Scope or acceptance criteria are ambiguous | Return the unresolved decision |

## Execution Steps

1. Read the objective, requirements, supplied artifacts and relevant project rules.
2. Identify the exact candidate or evidence being checked and its limitations.
3. Use the project's documented checks, only within the granted capabilities.
4. Compare observable output and side effects with the requested outcome.
5. Report passed, failed, skipped and unavailable checks without hiding gaps.

## Output Contract

Use the caller's exact result contract and preserve the verifier role and handoff
identity. Distinguish your observations from previously reported evidence.
Return `blocked` for missing required proof and `failed` for a demonstrated unmet
requirement. `completed` describes this assignment only; it is not a review
receipt or permission to mutate or deliver. Reference only available evidence;
do not fabricate test output or add fields unsupported by the supplied schema.

## References

Resolve these repository-relative paths from the supplied project root, not
from this skill's runtime discovery directory. If that root or a document is
unavailable, report the missing context; do not substitute another repository.

- Verification onboarding: `docs/skills/verification.md`
- Handoff contract: `docs/handoffs.md`
- Independent review: `docs/independent-review.md`
- Skill boundaries: `docs/skills.md`
