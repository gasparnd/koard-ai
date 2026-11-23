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

### Configure Engram at installation

For a fresh native installation with Engram 3.7.2 already on PATH:

```sh
koard native setup --runtime opencode --with-engram --dry-run
koard native setup --runtime opencode --with-engram
```

This adds the official, pinned Engram plugin at `.opencode/plugins/engram.ts`,
its MIT license/provenance under `.opencode/engram/`, and project-local
`mcp.servers.engram` configuration. No prior Engram host integration is needed.
Installation is offline and invokes only `engram version`, including in preview;
it does not download programs, run `engram setup`, start a server or open memory
storage. Missing binaries, other versions and conflicting nonblank `ENGRAM_BIN`
overrides refuse before publishing files. The currently verified combination is
Engram 3.7.2 and OpenCode 2.2.6; other Engram versions require a bundle update.

Both the plugin and MCP use `engram` on the OpenCode server's PATH. Leave
`ENGRAM_BIN` unset or exactly `engram`. They inherit the host's memory environment;
this does not create an isolated store. When the host loads the plugin, provider
behavior includes starting its local server when necessary, root-session
registration, prompt capture and import of `.engram/manifest.json` when present.
Do not point the plugin and MCP at different stores or endpoints.

**Configured is not activated or verified.** Inspect plugin and MCP status in
OpenCode before relying on continuity. OpenCode 2.2.6 keeps the first plugin ID,
so an existing global Engram plugin can win and mark the project copy duplicate.
Koard does not inspect every ambient plugin or remove global configuration.
A shared server can also have a different PATH/environment from the installer.

Static review of the pinned plugin also identified a continuity limitation:
events missed during a V2 event-stream disconnection are not replayed, so prompt
capture may have gaps without a degraded warning. Configuration tests do not
cover recovery from that runtime failure.

A bounded model-free probe of the generated bundle installed successfully and
created a real OpenCode root session, but plugin/MCP metadata remained empty and
Engram did not register that root during the 30-second observation. A later log
showed an MCP process spawn, not a confirmed connection. Native activation and
root attribution therefore remain unverified; the cause has not been established.

The opt-in uses the same no-overwrite contract. An earlier assets-only Koard
configuration differs and therefore refuses; automatic upgrades/merging remain
unimplemented. Without `--with-engram`, the original six-file bundle and its
dependency-free setup behavior are unchanged.

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

Engram needs a working host integration with authoritative session attribution.
`--with-engram` configures its official plugin and MCP locally; assets-only setup
still requires an independently configured integration. Session registration and
closure belong to the host plugin, not setup or Koard's per-run Engram subprocess.
Missing tools or attribution must be reported as unavailable continuity, not
successful persistence. Installation alone does not establish runtime proof.

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

### Deterministic proof without models

With OpenCode and Engram already installed, explicitly opt in to isolated
integration checks:

```sh
KOARD_NATIVE_PROOF=1 go test ./internal/adapters/engram ./internal/adapters/opencode -run '^TestNativeProof' -count=1 -v
```

The checks are skipped without that variable or with `go test -short`.
When opted in, missing binaries or unsupported APIs fail rather than silently
passing. No dependency installation, model prompt or production memory is used.
Each test owns temporary storage and bounded child processes; OpenCode uses a
fresh home/configuration/database and a generated local server password, not
the user's credentials. This is not a packet-level network-isolation claim.

Observed with OpenCode v2.2.6 and Engram 3.7.2:

| Check | Observed result | What it does not establish |
| --- | --- | --- |
| Real Engram MCP `mem_get_observation` | Exact 10,224-byte synthetic content, including its final marker | The OpenCode host calling that tool or binding a write |
| Real OpenCode session permission API | Both inspection agents deny `edit` and `shell`, and allow `read` | A tool invocation reaching the evaluator during a model turn |

The permission test installs the shipped bundle and uses server-returned root
and child session IDs. It calls the runtime's evaluator, not a reimplementation
of its matching rules. The MCP test reuses Koard's existing owned Engram adapter
to seed synthetic data, then reads through the actual MCP transport. These
results complement, rather than replace, the earlier real-host smoke.

The first exploratory API probe omitted local-server authentication and received
401. The public server contract documents `OPENCODE_PASSWORD` and Basic auth
with username `opencode`; the durable test uses a generated password for its
own server. No authentication bypass or ambient credential reuse is involved.

Configuration follows the official OpenCode V2 [agent](https://opencode.ai/docs/agents)
and [configuration](https://opencode.ai/docs/config) contracts. Runtime upgrades
need renewed compatibility evidence; other CLIs are not supported by setup yet.
