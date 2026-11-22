# Use Koard inside OpenCode

Install project-local orchestration assets, then work in a normal OpenCode V2
conversation. You do not need `koard odd run` for each request.

## Quick path

With Koard on your PATH, from the target project:

```sh
koard native setup --runtime opencode --dry-run
koard native setup --runtime opencode
```

Setup does not require `koard init`. It creates `.opencode/opencode.jsonc`,
three agent definitions under `.opencode/agents/`, and the `koard-odd` skill
with its guide under `.opencode/skills/`. Open a fresh OpenCode session and
confirm that `koard` is selected. Existing sessions keep their selected agent.
Ask for a bounded feature or explanation normally.

The primary agent works inline by default, uses native tools, and delegates
bounded inspection to `koard-explorer` or `koard-verifier`. Those children deny
tools by default and allow local discovery and skill loading; they cannot run
tests. The parent runs authorized checks. No model or provider is selected by
setup: your host configuration remains responsible for that choice.

## Preserve existing configuration

- Preview performs complete preflight without creating files.
- Repeating setup with identical destination bytes is a no-op. Modified files
  are never overwritten, even when an older Koard version generated them.
- A conflicting file, destination symlink or non-directory/symlink parent
  refuses preflight. Unrelated files are retained.
- Existing root `opencode.json`/`opencode.jsonc` or `.opencode/opencode.json`
  refuses setup. A differing `.opencode/opencode.jsonc` also refuses it. Manual
  adoption means reviewing the bundle source and integrating it yourself;
  automatic configuration merging and upgrades are not implemented.
- Files publish individually without replacing a destination. A late failure
  retains completed files; retrying the identical bundle converges. Setup is
  not an atomic multi-file transaction and cannot defend against hostile
  concurrent replacement of parent directories. A killed process may leave a
  `.koard-native-*` staging file; inspect it before manual removal.
- Global/ancestor configuration and managed policies are neither inspected nor
  rewritten. They can affect native activation and effective permissions.

Source assets live in `internal/adapters/opencode/nativeassets/`; generated
content identifies Koard and bundle version 1. Exit codes are 0 for success,
1 for installation/conflict errors, and 2 for invalid arguments. Partial
success paths print before a late error. No program, dependency, model or
global configuration is installed or launched.

## Hybrid authority and memory

The ODD skill guides authorization, exploration, proportional tracking,
implementation and verification. Instructions are not a sandbox. OpenCode
owns tools and permissions; repository instructions continue to apply.

Koard CLI checks remain callable through the host shell. Existing review,
consent and apply require real persisted workflow identities and their existing
gates. A receipt binds the exact stored snapshot; it does not approve arbitrary
native edits. Publishing and destructive actions remain separate user decisions.
No new live-edit receipt bridge is implemented by this setup command.

Engram must already have a working host integration with authoritative session
attribution. Setup does not configure MCP, register/end sessions or reuse the
per-run Koard Engram subprocess. Missing tools or attribution must be reported
as unavailable continuity, not successful persistence. Automatic Engram host
setup remains pending.

## Evidence and limits

Public-interface tests exercise setup, preview, conflict refusal, preservation,
repetition and installed skill validation. These checks do not prove that a
particular OpenCode installation loads or obeys the assets. Real-session
activation, delegation, permission enforcement and memory recovery remain
separate acceptance checks. This slice is not yet a verified end-to-end native
development installation.

### Observed native smoke (2026-11-22)

An isolated fixture on OpenCode v2.2.6 selected `koard` without an agent or model
override; native model selection resolved to `opencode/tidepool-alpha-free`.
Structured events confirmed loading the installed `koard-odd` skill and a
completed `koard-explorer` delegation. The primary implemented status filtering
and ran a failing test followed by five passing tests; an independent rerun of
those five tests also passed. The shipped assets and global configuration files
checked before and after the probe remained byte-identical.

The owned, isolated Engram store recorded the actual root session and synthetic
prompt, with no child session registration. However, the host turn did not finish
within 420 seconds and was interrupted before any observation save or resumed
recall. The explorer reported that writing was unavailable and created no canary;
there was no attempted write with an observed denial. Memory save/recovery and
that effective-permission probe therefore remain unverified. No global setup or
production-memory writes were part of the smoke. This evidence does not establish
automatic Engram installation, other models, or other OpenCode versions.

Two authorized follow-up turns reused the same model and isolated store, with
180-second limits. The first completed successfully: a real `mem_save` stored
one synthetic observation, attributed by the host adapter to its authoritative
root-session continuation. The second made a real `mem_search` that returned
that observation and fact, demonstrating persisted-memory access across
continuations, but timed out before full `mem_get_observation` retrieval or the
permission canary. A separate read-only agent-inspection API request returned
HTTP 404. No additional model attempts were made. Global configuration and
shipped bundle hashes remained unchanged, and all owned processes stopped.

Thus save and search continuity are verified with the already installed Engram
host adapter; full retrieval and attempted-write denial remain unverified.
This does not add automatic Engram configuration to Koard's installer.

Configuration follows the official OpenCode V2 [agent](https://opencode.ai/docs/agents)
and [configuration](https://opencode.ai/docs/config) contracts. Runtime upgrades
need renewed compatibility evidence; other CLIs are not supported by setup yet.
