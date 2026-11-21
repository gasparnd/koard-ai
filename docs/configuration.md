# Project configuration

Koard reads project-level settings from `.koard/koard.yaml`. The configuration is versioned so the CLI can reject incompatible files instead of silently interpreting them incorrectly.

## Supported schema

The current CLI supports schema version `1` only:

```yaml
schema_version: 1
project:
  name: koard
workflow:
  default: odd
memory:
  provider: engram
agents:
  planner:
    provider: unset
    model: unset
  executor:
    provider: unset
    model: unset
  verifier:
    provider: unset
    model: unset
```

## Validation rules

An optional `sync` section declares native skill projections explicitly:

```yaml
sync:
  skills:
    - name: koard-planning
      runtimes: [opencode, codex]
```

Omitting it, or using `skills: []`, selects nothing. Every entry requires a
nonempty unique `name` and a nonempty list of nonempty unique `runtimes`.
Unknown nested fields are rejected. These structural checks apply to `doctor`
and every configuration consumer; `sync` additionally validates supported
runtimes, portable names and filesystem availability before writing. It does
not derive the list from `agents` or run automatically with workflows. See
[the sync walkthrough](sync.md) for use cases, output and recovery.

`koard doctor` requires:

- exactly one YAML document;
- `schema_version: 1`;
- no unknown fields;
- a non-empty project name;
- a non-empty default workflow;
- a non-empty memory provider;
- non-empty provider and model values for planner, executor, and verifier.

The literal value `unset` is valid during bootstrap. It records that role selection remains intentionally unresolved without weakening structural validation.

A resolved `provider` names a runtime adapter. `koard review run` currently accepts `opencode` and `codex` for `agents.verifier`, and fails closed on any other value — including `unset` — because review cannot run without a chosen runtime. See [codex-adapter.md](codex-adapter.md).

## Check a project

`memory.provider: engram` enables automatic project-scoped recall and selective
retention during `koard run`. `none` disables memory without changing routing.
Other nonblank values still pass structural validation but produce a memory
warning during a run. The storage directory is the absolute `ENGRAM_DATA_DIR`
override or `~/.engram`; it is not a repository YAML field. See [memory.md](memory.md).

`koard run` uses the same provider choices for each destination role: executor and verifier for `fixed`, plus planner for `planned`. Required roles must have a supported provider and a model other than `unset`; an unused planner may remain unresolved. `--policy` selects routing without reinterpreting `workflow.default`. See [routing.md](routing.md).

```bash
go run ./cmd/koard doctor
```

Success prints the validated schema version and exits with code `0`. A missing or invalid configuration prints an actionable error to standard error and exits with code `1`.

## Compatibility policy

Adding a future schema version requires an explicit parser and migration policy. Koard must not silently reinterpret an older or newer configuration as version `1`.

`sync` is an optional addition to schema version 1. Existing files remain valid
without modification. Older Koard builds with strict parsers reject this new
field; upgrade the executable before adding it. The command never edits the
configuration itself.
