---
name: koard-planning
description: "Trigger: plan a Koard task, inspect requirements, prepare a bounded proposal. Ground the plan in project evidence."
license: Apache-2.0
metadata:
  author: koard-project
  version: "1.0"
---

## Activation Contract

Use when assigned a planning task with an objective and bounded context. Do not
treat discovery of this skill as an instruction to start a workflow.

## Hard Rules

- Respect the supplied capabilities, output schema and task identity.
- Read project instructions and relevant source before proposing changes.
- Separate observed facts, assumptions and unresolved decisions.
- Do not edit files, install tools, grant consent or invoke downstream roles.
- A plan does not authorize execution. Never invent files, tests or results.

## Decision Gates

| Evidence | Action |
|---|---|
| Objective and scope are clear | Propose the smallest coherent plan |
| Required context is unavailable | Report the missing evidence and block |
| A consequential product choice is unresolved | Return the exact decision needed |
| A technical premise can be checked within scope | Inspect it before assuming |

## Execution Steps

1. Identify the requested outcome, non-goals and allowed inspection scope.
2. Inspect applicable project instructions, manifests, source and test commands.
3. Map only the affected behavior and identify existing helpers to reuse.
4. Propose ordered steps with observable acceptance criteria and relevant checks.
5. Name risks, missing proof and the next decision without claiming authority.

## Output Contract

Use the caller's exact result contract. In a Koard handoff, preserve its identity
and planner role; put the concise plan in the allowed summary field and reference
only existing inspected artifacts. Do not add unrecognized plan fields. Report
`blocked` when required context or a decision is missing. `completed` means the
planning assignment is complete, not that implementation or delivery occurred.

## References

Resolve these repository-relative paths from the supplied project root, not
from this skill's runtime discovery directory. If that root or a document is
unavailable, report the missing context; do not substitute another repository.

- Planning onboarding: `docs/skills/planning.md`
- Handoff contract: `docs/handoffs.md`
- Skill boundaries: `docs/skills.md`
