# Skill style guide

A Koard skill is a concise runtime instruction contract for an AI agent. It is not a general tutorial.

## Required layout

```text
skills/<skill-name>/
├── SKILL.md
├── assets/       optional templates or schemas
└── references/   optional supporting documentation
```

## Required frontmatter

```yaml
---
name: skill-name
description: "Trigger: exact phrases. State what the skill does."
license: Apache-2.0
metadata:
  author: koard-project
  version: "1.0"
---
```

## Required sections

Use this order:

1. Activation Contract
2. Hard Rules
3. Decision Gates
4. Execution Steps
5. Output Contract
6. References

Keep `SKILL.md` focused on executable instructions. Move long explanations to
`references/` or a linked onboarding page under `docs/skills/`, and reusable
templates to `assets/`. Explain each skill's purpose, activation, inputs, outputs,
limits and evaluation examples for contributors.

The registry discovers project skills from disk; no manual index is required.
Run `koard skill validate --strict` and register each project skill in `AGENTS.md`
until runtime projection is implemented. Strict validation checks required
metadata and the ordered headings, not the semantic quality of instructions.
Third-party skills use the compatible validator instead; see [skills](skills.md).
