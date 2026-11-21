# Keep declared skill integrations ready with `koard sync`

`koard sync` creates the native skill projections explicitly listed in your
project configuration. Use it after cloning a project or adding a skill to that
list. You maintain the original skills once; teammates can reproduce the same
integration layout without projecting each skill manually.

This first version manages **skill links only**. It does not download a new
Koard version, rewrite runtime settings, change permissions, or run an agent.

## Quick path

Start in your project root. Initialize Koard if needed, then add this optional
section to your existing `.koard/koard.yaml` (do not replace the rest of the file):

```yaml
sync:
  skills:
    - name: koard-planning
      runtimes: [opencode, codex]
    - name: koard-verification
      runtimes: [codex]
```

These example skills must already exist in your project's active catalog.
Use your own skill names for a different project. Drafts and global-only skills
are not eligible. See [creation and activation](skills/create.md).

Preview before writing:

```sh
koard sync --dry-run
```

For three missing projections, expect three `Would project` lines followed by:

```text
Sync preview: 3 to create, 0 unchanged.
```

Apply the declared projections:

```sh
koard sync
```

The command prints each processed destination and source, then:

```text
Sync complete: 3 created, 0 unchanged.
```

Repeating it prints `Already projected` for matching destinations and ends with
`Sync complete: 0 created, 3 unchanged.` The original source files and your YAML
remain unchanged. Counts reflect the current filesystem, not a stored manifest.

## What the declaration means

| Declaration | Result |
|---|---|
| `name: koard-planning` | Select that name's winning project registry entry |
| `runtimes: [opencode]` | Link it at `.opencode/skills/koard-planning` |
| `runtimes: [codex]` | Link it at `.agents/skills/koard-planning` |
| Both runtimes | Check and process both destinations, in listed order |
| No `sync` section or `skills: []` | Print `No skills configured for sync.` and write nothing |

Each skill name must appear once. Its runtime list must be nonempty and contain
no duplicates. `sync` currently supports exactly `opencode` and `codex`. Names
follow the portable lowercase-hyphenated format, at most 64 characters. The
command reuses the [projection rules](skills/project.md), including conflict
refusal, relative links and source containment.

The list is independent of `agents.*.provider`. Adding a runtime for native
discovery does not change which provider runs a Koard role. Conversely, changing
a role provider does not silently expose new skills to that runtime.

## Common use cases

### A teammate clones the repository

Commit your authored skills and the explicit configuration through your normal
review process. Your teammate installs Koard, previews `sync`, and applies it.
The command recreates missing links without needing model credentials. If your
checkout already contains the matching links, it leaves them alone.

### You add a payment-review skill

Create and complete the draft, review its instructions, and activate it in the
project catalog. Add an entry such as:

```yaml
    - name: review-payments
      runtimes: [codex]
```

Place that entry under `sync.skills`, then preview and synchronize. Other new
skills remain unprojected until explicitly listed. The command neither authors
the instructions nor approves their content.

### You stop using a runtime

Removing it from the list stops future creation, but **does not delete existing
links**. Inspect obsolete links and remove only those links yourself if desired.
Koard cannot assume that removing a declaration authorizes deleting files.

### You update Koard itself

Install the new executable using your chosen installation method. Running
`sync` afterward checks this project's declared skill links; it does not perform
the program update or a general runtime-configuration migration.

## Conflicts, interruptions and recovery

The complete list is checked before the first write. An unknown runtime, missing
skill, invalid catalog or existing conflicting destination stops preflight with
exit 1 and no projection writes. Even a valid first entry is not applied when
a later entry fails these checks.

Application is not a multi-file transaction. Permissions, disk errors or changes
after preflight can still stop a later operation. In that case, output lists the
completed operations and the error reports how many were processed. Successful
links are retained; no rollback deletes them. Resolve the cause, preview again
and rerun: already-correct links become no-ops.

| Symptom | Next action |
|---|---|
| Configuration missing | Run `koard init`, then edit the explicit list |
| Configuration invalid | Run `koard doctor`; fix the named field or duplicate |
| Skill not found | Check `koard skill list`; activate it or correct the name |
| Invalid skill catalog | Run `koard skill validate` for detailed diagnostics |
| Destination exists | Inspect it; sync will not overwrite, merge or retarget it |
| Directory links unavailable | Check filesystem support/permissions; there is no copy fallback |

`doctor` checks configuration structure, including required list values and
duplicates. `sync` additionally checks portable names, supported runtimes,
installed sources and destinations. An older Koard build that does not support
the optional `sync` field will reject it as unknown; existing configurations
without that field continue to work unchanged.

## Boundaries you should know

- Only the declared pairs are processed. Empty lists do not scan or synchronize
  all skills. Unrelated files, native settings and global installations are untouched.
- Codex's `.agents/skills` is also discovered by OpenCode and other compatible
  runtimes. A target chooses a location, not exclusive visibility.
- A source edit is visible through the link without rerunning sync. Moving or
  deleting the source can break it; sync refuses stale/conflicting links rather
  than silently replacing them.
- Review parent-relative skill references before native use. See the
  [reference guidance](skills/project.md#keep-references-usable).
- Preview is not a frozen approval: the configuration and filesystem are checked
  again on the later apply invocation. A running sync uses the configuration it
  loaded at startup. No lock or protection against hostile rename races is claimed.
- Nothing commits, publishes, starts a runtime or invokes a model. Native
  discovery and model compliance need separate runtime checks. Koard's workflow
  sandbox, review consent and automatic instruction delivery remain unchanged.

Exit codes: **0** success, including no work; **1** configuration or projection
failure; **2** invalid command arguments. `koard sync --help` requires no project.

## Contributor map

`internal/config` parses the optional declaration. `internal/cli/sync.go` resolves
the runtime adapters and renders results. `internal/application/skillsync` does
the complete preflight and ordered application by reusing `skillprojection.Project`;
it does not implement a second link-creation policy.

Public CLI tests cover explicit selection, preview, repeat calls, late-list
conflicts, legacy configuration and preservation of removed declarations.
Application tests cover an injected post-preflight conflict and safe retry.
Run `go test ./...` and `go vet ./...` after changes. Keep examples and help
synchronized with observed output, and distinguish filesystem proof from native
runtime/model validation.
