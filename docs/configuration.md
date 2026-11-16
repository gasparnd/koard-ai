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

```bash
go run ./cmd/koard doctor
```

Success prints the validated schema version and exits with code `0`. A missing or invalid configuration prints an actionable error to standard error and exits with code `1`.

## Compatibility policy

Adding a future schema version requires an explicit parser and migration policy. Koard must not silently reinterpret an older or newer configuration as version `1`.
