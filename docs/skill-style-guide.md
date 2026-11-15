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

Keep `SKILL.md` focused on executable instructions. Move long explanations to `references/` and reusable templates to `assets/`. Register each project skill in the future Koard skill registry and in `AGENTS.md` until registry projection is implemented.
