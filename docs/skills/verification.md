# Verify outcomes with `koard-verification`

Verification prevents a fluent completion message from being mistaken for
working behavior. This skill asks an agent to compare requirements with concrete
evidence and to state what it could not prove.

The executable instructions are in
[`skills/koard-verification/SKILL.md`](../../skills/koard-verification/SKILL.md).
Use this page to understand the rationale and evaluate contributions.

## Inputs and outputs

The verifier needs the objective, acceptance criteria, exact artifact references,
available evidence, project check commands and granted capabilities. Its result
identifies passed requirements, failures and missing proof using the caller's
existing schema. It does not invent a second reporting protocol.

| Finding | Expected response |
|---|---|
| A required public behavior was observed | State the check and observed result |
| Another actor reports a successful test | Attribute it; do not claim an independent run |
| Required checks cannot run under current capabilities | Report blocked proof, without broadening permissions |
| Observed behavior contradicts the requested outcome | Report failure with the relevant evidence |

## Example

**Assignment:** verify that invalid CLI input returns exit code `2`, produces an
actionable stderr message and leaves the workspace unchanged.

A verifier with permission to run the isolated test checks all three effects.
Exit code alone is insufficient. A verifier permitted only to read files can
inspect the test and supplied results, but must distinguish that readback from
executing the test itself. If independent execution is required and unavailable,
the outcome is blocked, not a fabricated pass.

This distinction matters in Koard today: proposal adapters allow repository
inspection, not arbitrary test execution. Loading a skill cannot expand that
capability set or make a write-requiring test safe.

## Verification is not independent review

Requirements verification checks whether the requested behavior is supported by
evidence. Independent review examines a frozen candidate through its separate
consent and review lifecycle. A verifier's `completed` result is neither an
approved review verdict nor a receipt. Koard validates those artifacts and
controls their transitions in code.

The end-to-end fixture validation illustrates why this matters: the workflow,
receipt and apply succeeded, but the snapshot contained unchanged bytes. That
proves governed coordination and application, not generation of modified code.

## Contribute or evaluate

After edits, run `koard skill validate --strict` from the repository. Evaluate
examples with complete proof, missing test execution and a demonstrated failure.
Check that the instructions preserve all three distinctions. Explicitly label
text-only evaluation versus a real model run; structural checks cannot establish
that an arbitrary model follows the skill.

Keep project-specific test commands in project documentation, not in this
general skill. Add new examples here instead of growing the agent instructions
into a tutorial. A new output field requires a separate contract change and
tests; do not introduce it through prose.

## Current integration limit

`run` and `odd run` may automatically select this skill and deliver its exact
captured instructions; selection is contextual rather than forced by role.
Use [native projection](project.md) for OpenCode or Codex. Documentation paths
in the skill resolve from the supplied project root, not its projected directory.
Neither selection nor manual
loading changes sandbox permissions, review consent or delivery authority.
See [skills](../skills.md), [handoffs](../handoffs.md) and
[independent review](../independent-review.md).
