# Create a skill without activating an unfinished draft

Use `koard skill create <name>` when a recurring task needs reusable agent
guidance. It creates a deterministic template and an onboarding guide, not
model-written expertise. No provider, credentials or initialized Koard config
is required. Existing installed skills remain unchanged.

## Quick path

With the Koard executable available on PATH, start in your project root:

```sh
koard skill create api-review
```

Expected output begins with:

```text
Draft created: .koard/skill-drafts/skills/api-review
```

Two editable files are created:

```text
.koard/skill-drafts/skills/api-review/
├── SKILL.md    instructions for the agent
└── README.md   onboarding for the contributor
```

`koard skill list` from the project root will not list this draft. `run` and
`odd run` capture the same catalog, so creation does not make it selectable.
The nested `skills/` directory lets you reuse the existing validator by
explicitly choosing the draft workspace, without changing discovery rules.

## Edit: teach the task, not just the command

Replace every TODO in both files. Select the appropriate license and author;
the scaffold deliberately does not choose a license for your content.

| File | What a new contributor should understand |
|---|---|
| SKILL.md | Activation, non-goals, hard rules, decisions, steps and expected output |
| README.md | Purpose, audience, prerequisites, inputs/outputs, worked example, limits and maintenance |

For example, an API review skill might activate for “review this endpoint
change,” accept a proposed API diff and documented compatibility policy, and
return findings with evidence. Its negative example could be an unrelated UI
styling task. Define concrete expected findings for a small fixture rather than
claiming that a structurally valid skill guarantees a successful review.

Keep framework policy in a dedicated skill when needed; general planning and
verification methods should remain technology-neutral. See the
[style guide](../skill-style-guide.md) and existing
[planning](planning.md) and [verification](verification.md) guides.

## Validate: structure is not approval

```sh
(cd .koard/skill-drafts && koard skill validate --strict)
```

With only this draft present, expect `Validated 1 skills.` This command checks
all drafts in that workspace, not just api-review. The scaffold already has
valid metadata and section order: TODOs can still pass. Check their removal,
exercise your positive and negative examples, and have a human review the
instructions. Validation invokes no model and cannot assess instruction quality.

## Activate: a deliberate manual step

After review, inspect `koard skill list` for conflicting names. Move the whole
reviewed directory from `.koard/skill-drafts/skills/api-review/` to
`skills/api-review/`, preserving both files. Create the parent if needed, but
refuse an existing destination: do not merge or overwrite another skill.

There is no `skill activate` command or stored approval record. Placement in a
discovery root makes a valid skill eligible for future automatic selection;
it is not proof of review. Do not start a workflow against the draft workspace
unless you intentionally want its drafts treated as that workspace's skills.

From the original project root, run:

```sh
koard skill validate --strict
koard skill list
```

Confirm the listed source path, then link the skill and its guide from your
project's AGENTS.md and contributor documentation. Creation never edits those
indexes. Selection is contextual, not guaranteed; `--skills=off` bypasses it.
Moving a skill does not change instructions already frozen in an earlier run.

## Refusals and recovery

Names must be lowercase alphanumeric words separated by single hyphens, at most
64 characters. Paths, extra arguments and flags other than `--help`/`-h` are
rejected with exit 2 before writing. Success exits 0; filesystem errors exit 1.

Creation exclusively claims a new draft directory. Existing destinations,
including empty directories and interrupted drafts, are refused unchanged.
Existing symlinks or non-directories in the fixed parent path are rejected.
It does not overwrite active skills, write configuration, register a runtime
skill, invoke models, or run git.

Writes are not an atomic two-file transaction. A failure can leave parent
directories or a partial draft; inspect and recover it manually rather than
blindly retrying. Existing-link checks are not a sandbox against hostile
concurrent directory replacement. Use a trusted local workspace.

## Contributor implementation map

- `internal/cli/skill_create.go`: argument checks and exclusive draft creation.
- `internal/cli/templates/`: embedded deterministic templates; change instructions
  and human guidance together.
- `internal/cli/skill_create_test.go`: public CLI creation/refusal checks, existing
  content preservation, strict validation and exclusion from capture.
- `internal/skillregistry/`: unchanged discovery and validation implementation.

Run `go test ./...` and `go vet ./...` after changing the generator. Evaluate
real-model relevance separately; deterministic tests prove filesystem and
transport behavior, not the quality of an authored skill.
