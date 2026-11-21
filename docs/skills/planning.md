# Plan a bounded task with `koard-planning`

Planning reduces the chance of solving the wrong problem. This skill asks an
agent to establish scope and inspect evidence before proposing work. It is
useful when an objective is clear enough to investigate but the affected code,
checks or unresolved decisions still need to be identified.

The executable instructions are in
[`skills/koard-planning/SKILL.md`](../../skills/koard-planning/SKILL.md).
This page explains the reasoning for contributors; it is not a replacement for
the skill or a second set of runtime rules.

## Inputs and outputs

| Input | Why it matters |
|---|---|
| Objective and acceptance criteria | Defines the requested result, not just an activity |
| Capabilities and context references | Defines what the planner may inspect |
| Project instructions, source and manifests | Establishes actual conventions and technologies |
| Caller-provided result schema | Keeps output portable across model runtimes |

The result is a concise plan, affected behavior, applicable checks and explicit
unknowns. When used with Koard's handoff contract, the plan fits the existing
summary and reference fields. The skill does not add a new JSON schema or a
`plan` field that the strict decoder would reject.

## Example

**Assignment:** plan validation of a new CLI option, without modifying files.

The planner reads the option parser, its public tests and contribution guide.
It identifies the intended valid/invalid inputs, the existing parser helper,
and tests for exit code, stdout and stderr. Its proposed sequence is to reproduce
the missing validation, implement the bounded change and rerun the relevant
checks. It says whether another command shares the helper.

If the required parser file is unavailable, it reports the missing reference
instead of inventing its behavior. If the accepted input range is unspecified,
it identifies that product decision instead of choosing a range silently.

## Why these rules exist

- **Inspect before planning:** remembered architecture can be stale.
- **Separate assumptions from facts:** an unverified premise must not become a
  requirement just because a model stated it confidently.
- **Reuse project conventions:** the same method works for Go, TypeScript or a
  documentation-only task without embedding language-specific advice.
- **Respect the output contract:** typed artifacts, not free-form conversation,
  connect Koard roles. A plan is useful only if the next actor can consume it.
- **Keep authority separate:** completion of planning never grants permission
  to implement, review, apply or publish.

## Contribute or evaluate

Run `koard skill validate --strict` from this repository after editing the skill.
Check a clear task and a task missing essential context: the former should yield
a grounded bounded plan; the latter should identify the gap. Record whether you
actually invoked a model or only reviewed the text. Structural validation alone
does not prove model compliance.

Update this guide when the skill's intent changes, keeping detailed examples
here and concise executable instructions in `SKILL.md`. Do not silently change
the handoff schema or embed framework policy in the skill.

## Current integration limit

`run` and `odd run` may automatically select this skill for a relevant assignment
and append its exact captured instructions. Selection is contextual, not forced
by role. `--skills=off` disables it. A host agent may also explicitly load it
under that host's permissions. Use [native projection](project.md) for OpenCode
or Codex. The skill's documentation paths explicitly resolve from the supplied
project root, not its projected directory. See [the registry guide](../skills.md)
for costs and limits; projection never grants planning or execution authority.
