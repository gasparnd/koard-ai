# Use one skill source in OpenCode and Codex

Use `koard skill project` when you want an existing project skill available in
a runtime's own interactive sessions, outside `koard run` and `koard odd run`.
It creates a relative directory symlink, not a second copy of the instructions.
Edit the original skill once; the native discovery path sees those same files.

For a repeatable list of skill/runtime pairs, use [`koard sync`](../sync.md).
It reuses this projection mechanism with complete-list preflight; this command
remains useful for one explicitly selected skill.

## Quick path

From the project root, with `koard` on PATH and an existing `koard-planning`
skill, preview the destination before creating it:

```sh
koard skill project --runtime opencode --dry-run koard-planning
koard skill project --runtime opencode koard-planning
koard skill project --runtime codex koard-planning
```

The preview prints `Would project`, followed by the destination and source.
Creation prints `Projected`. Running the same projection again prints
`Already projected` and does not replace the link. Options must precede the name.
No configured provider, credentials or Koard configuration file is required.

| Runtime target | Created discovery path | Visibility |
|---|---|---|
| OpenCode | `.opencode/skills/koard-planning` | OpenCode project discovery |
| Codex | `.agents/skills/koard-planning` | Codex and other compatible runtimes, including OpenCode |

Runtime selection chooses a location, **not exclusive visibility**. If you use
both targets, OpenCode may discover both paths. Its ID/precedence rules and the
host's permissions decide the loaded definition. Koard does not change them.

## Inputs, outputs and prerequisites

- Input: one installed project skill name and `--runtime opencode|codex`.
- Output: one relative directory link, plus missing discovery parent directories.
- Source: the winning entry from Koard's existing project-only registry. Invalid
  catalogs fail before writing; use `koard skill validate` to inspect diagnostics.
- Name: lowercase alphanumeric words separated by single hyphens, at most 64
  characters, so the destination basename is also a portable native skill ID.
- Filesystem: directory symlinks must be supported and permitted. Windows may
  require Developer Mode or additional privileges; failures do not fall back to
  copying files or changing privileges.

Global skills and inactive drafts are not selected. Complete, review and manually
activate a draft first using the [creation guide](create.md). Invoke projection
from the actual project root, not the isolated draft validation directory.

## Keep references usable

Linking the **whole directory** exposes its sibling assets and references without
copying them. File contents are never rewritten, bundled, executed or approved.
Native loaders do not necessarily read supporting files automatically.

OpenCode can retain the projected directory as the skill's base. A reference
such as `../../docs/guide.md` may then resolve differently, depending on the
reader. Do not assume directory symlinks make parent-relative links portable.

For project documentation, state explicitly that paths such as `docs/guide.md`
are relative to the supplied project root. Koard's planning and verification
skills use that convention and report missing context rather than guessing a
different repository. Keep truly local resources inside the skill directory.
Review external skills before projecting them; the command does not parse or
repair every reference and cannot prove semantic portability.

## Verify and maintain

1. Use `koard skill list` to confirm the original winning source. After projection,
   shadow warnings can name the additional native path; the source still wins.
2. Check that `SKILL.md` and a local reference are readable through the link.
3. In a separately authorized native session, check the runtime's skill selector
   and explicitly load the skill. Discovery is not proof that a model will select
   it automatically or follow it correctly.
4. Edit the original source, never maintain a separate projected copy. Keep its
   onboarding guide and examples synchronized.

The link is relative, so moving the whole project preserves it. Moving or removing
only the source breaks it; repair the layout deliberately. A matching existing
link (or a source already at the native destination) is a no-op. Any conflicting
file, directory or link is refused, including dangling links. There is no force,
overwrite, removal or automatic retargeting option. To withdraw a projection,
inspect and remove only the link yourself, never the source directory.

## Safety and failure behavior

`--dry-run` checks the source and destination but writes nothing. Success exits
0, usage errors exit 2, and catalog/filesystem conflicts exit 1. Existing
symlinked destination parents are refused. Sources must remain within the
project and contain a regular SKILL.md, not an individually symlinked entry file.

This is a trusted-local-workspace operation, not a sandbox against hostile
concurrent directory replacement. A failed link creation can leave newly created
parent directories, but no existing destination is overwritten. Inspect the
reported error and retry after resolving the cause. No runtime is launched, no
settings are changed, and no git, network or global installation is performed.

Projection makes native discovery possible under the native runtime's own
permissions. It does **not** enable OpenCode's skill loader inside Koard proposal
runs, change Codex sandbox flags, grant execution/review authority, or alter the
frozen instruction delivery used by `run` and `odd run`.

## Contributor map and evidence

`internal/application/skillprojection` reuses `skillregistry.Scan` and owns the
non-overwriting link operation. Runtime adapters implement the small `Target`
interface to supply native directories; the application imports no providers.
`internal/cli/skill_project.go` composes them. Public CLI tests cover preview,
identity, idempotency, refusal, supporting files and unchanged registry behavior.

Native discovery choices follow the official [OpenCode skills guide](https://opencode.ai/docs/skills)
and [Codex skills guide](https://developers.openai.com/codex/build-skills).
OpenCode's symlink traversal and retained source spelling were inspected in
[v2.1.2 skill-scan.ts](https://github.com/anomalyco/opencode/blob/a517687fde98a7329ada3ae644a01a9fc6c4e99e/packages/core/src/config/plugin/skill-scan.ts)
and [skill-file.ts](https://github.com/anomalyco/opencode/blob/a517687fde98a7329ada3ae644a01a9fc6c4e99e/packages/core/src/config/plugin/skill-file.ts).
This is documentation/source compatibility evidence, not a live-model test or a
claim about every installed runtime version. Run `go test ./...` and
`go vet ./...` after changing the implementation.
