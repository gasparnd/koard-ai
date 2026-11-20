# Discover and validate portable skills

A skill is an instruction contract for an agent, not a permission grant or an
executable workflow. Koard's registry discovers existing `SKILL.md` files and
validates their structure without executing, installing, copying, or loading them
into a model. It does not require Gentle AI or a Koard project configuration.

## Quick start

Run from the project directory; Koard does not search parent repositories:

```sh
go run ./cmd/koard skill list
go run ./cmd/koard skill list --json
go run ./cmd/koard skill validate
go run ./cmd/koard skill validate --strict
```

For installed binaries, replace `go run ./cmd/koard` with `koard`. Opt into
supported directories under your home directory with `--global`. Without that
flag, discovery does not include global skills, including through symlinks.

`list` and `validate` both report invalid candidates. `validate --strict` adds
Koard's authoring requirements; use ordinary validation for third-party skills.
Strict mode applies to **every discovered candidate**, not only the winners or
skills with a `koard-` prefix. Both commands accept `--json` and `--help`.

## What the registry does

```text
Explicit project directory + optional home directory
  → ordered skill roots
  → direct skills, then one category level
  → bounded reads and YAML validation
  → first-name winners + diagnostics
  → human-readable or JSON report
```

Each winner carries its name, full description, absolute source path and scope.
Read the original `SKILL.md` before using it; the registry is not a compressed
replacement for its instructions. A description is a discovery hint, not proof
that a skill is appropriate or safe.

JSON has two arrays, `skills` and `diagnostics`, including when they are empty.
Diagnostics carry `severity`, `code`, `path`, and `message`. Duplicate names
produce `warning` / `shadowed`; malformed or inaccessible candidates produce
errors. Partial results remain available, but an error makes the exit code `1`.
Success is `0`; unsupported commands, flags and positional arguments return `2`.
Text diagnostics go to stderr; JSON embeds them in the stdout report.

## Discovery and precedence

Roots are searched in this order. Paths are relative to the indicated base:

| Scope | Ordered roots |
|---|---|
| Project | `skills`, `.koard/skills`, `.opencode/skills`, `.claude/skills`, `.gemini/skills`, `.trae/skills`, `.cursor/skills`, `.github/skills`, `.codex/skills`, `.qwen/skills`, `.kiro/skills`, `.openclaw/skills`, `.pi/skills`, `.agent/skills`, `.agents/skills`, `.atl/skills`, `.hermes/skills` |
| Home, only with `--global` | `.koard/skills`, `.pi/agent/skills`, `.config/agents/skills`, `.agents/skills`, `.kimi-code/skills`, `.kimi/skills`, `.config/opencode/skills`, `.config/kilo/skills`, `.claude/skills`, `.gemini/skills`, `.gemini/config/skills`, `.gemini/antigravity/skills`, `.cursor/skills`, `.copilot/skills`, `.codex/skills`, `.codeium/windsurf/skills`, `.qwen/skills`, `.kiro/skills`, `.openclaw/skills`, `.hermes/skills`, `.trae/skills` |

- Project candidates precede global candidates; first valid name wins.
- Within a root, direct `name/SKILL.md` files precede `category/name/SKILL.md`.
  Each level is sorted by path. Discovery never descends below a direct skill
  or beyond one category level. Final winners are sorted by name.
- Names are case-sensitive. Frontmatter `name` wins; the directory basename is
  the fallback. Invalid candidates are reported even when another source wins.
- Missing roots are normal. Broken symlinks and unreadable roots are errors.
- Root, directory and file symlinks work only when resolved targets remain in
  the selected roots. A project symlink into a supported global root requires
  `--global`; a link to an arbitrary external directory remains refused.
  Scope describes where the entry was discovered, not a trust level.
- Custom runtime locations (`CODEX_HOME`, `XDG_CONFIG_HOME`, remote catalogs)
  are not resolved in this version. The CLI uses the standard home locations
  above, not runtime-specific configuration discovery.

Gentle AI inspired project-first discovery, one category level and source-path
preservation. Koard deliberately does **not** read or overwrite
`.atl/skill-registry.md`, apply Gentle-specific exclusions, or silently suppress
invalid files. Existing installed files are reused in place. See the upstream
[registry documentation](https://github.com/Gentleman-Programming/gentle-ai/blob/main/docs/skill-registry.md)
for inspiration, not a version-pinned compatibility promise.

## Compatibility versus authoring

Compatible skills need UTF-8 Markdown with a delimited YAML frontmatter object,
a nonempty description and instruction body. Unknown metadata is accepted;
multiline YAML descriptions work. Names cannot contain path separators or
control characters. YAML duplicate keys and malformed documents are errors.

Strict authoring additionally requires an explicit lowercase hyphenated name,
license, `metadata.author`, `metadata.version`, and the six ordered level-two
headings from the [style guide](skill-style-guide.md). Ordinary triple-backtick
examples are ignored; indented or mixed/longer fences can be misclassified by
the current lightweight heading parser. These checks verify structure, not instruction quality,
truthfulness or model compliance. Do not impose Koard-specific headings on
existing third-party skills just to discover them.

Reads are limited to 64 KiB per regular file and 4,096 entries per visited
directory. Nonregular files and symlink escapes are rejected. Discovery is not
a sandbox against a concurrently hostile filesystem; do not scan untrusted
mounts while another process replaces their directories. No skill body is
included in the registry report and no instruction is executed.

## Initial Koard skills

| Skill | Purpose | Contributor guide |
|---|---|---|
| `koard-planning` | Turn a bounded objective into a plan grounded in project evidence | [Planning](skills/planning.md) |
| `koard-verification` | Check the requested outcome and distinguish observed proof from missing proof | [Verification](skills/verification.md) |

They are language- and runtime-neutral. They direct agents to inspect the
project's conventions and test commands instead of embedding Go, React or
NestJS policy. They do not teach a model an unavailable capability.

## Architecture and contributing

`internal/cli/skill.go` parses options and renders reports.
`internal/skillregistry` owns ordered discovery, bounded filesystem reads,
frontmatter parsing and structural validation. It imports no runtime adapter,
router, memory provider or Gentle AI code. The package uses the repository's
existing YAML dependency. No configuration schema or handoff schema changes.

To add a skill:

1. Follow the [style guide](skill-style-guide.md), with a concise `SKILL.md`.
2. Add a human guide explaining why it exists, activation, inputs, outputs,
   limits and a realistic example. Link it from the skill's References.
3. Register it in `AGENTS.md` and this page for contributor discoverability.
4. Run compatible and strict validation for the project's authored skills.
5. When changing discovery, add public CLI tests in `internal/cli/skill_test.go`;
   use temporary project/home directories, never actual user skill folders.
6. Run `go test ./...` and `go vet ./...` and update this reference alongside
   any CLI or precedence change.

## Automatic selection during workflows

`run` and `odd run` automatically select relevant skills from project roots.
Listing remains read-only and metadata-only: JSON is the catalog interface,
while `SKILL.md` remains the instruction source. No Gentle AI installation,
generated registry or per-role skill list is required.

```sh
koard run --workflow example --task task --objective "Inspect the proposed change"
koard odd run --workflow example-global --task task --objective "Inspect tests" --global-skills
koard run --workflow example-off --task task --objective "Inspect tests" --skills=off
```

These commands invoke your configured models. `--skills=auto` is the default;
`--skills=off` skips discovery, selection and skill audit entirely. Global roots
remain opt-in through `--global-skills`, ignored when selection is off. The
`skill list`/`validate` commands retain their separate `--global` option.

### What happens for each role

1. Inside the exclusive workflow claim, capture the validated winning files
   once. Invalid catalogs stop the run, even when another valid skill exists.
2. Before a main handoff, use that role's configured runner for one selection
   request. It receives the objective, role, project/context references and
   catalog metadata, not the skills' instruction bodies.
3. Accept zero to five unique catalog IDs with exact digests. Unknown IDs,
   duplicate entries, other artifacts, evidence, transitions and memory
   proposals are refused. Selection never chooses a provider or file path.
4. Append the selected complete source text to the main Objective and persist
   that exact handoff before execution. Preserve memory warnings, verifier
   guidance, identities, context, capabilities and the result schema.

There is no technology-specific classifier: the configured model decides
relevance from the supplied task and project evidence. It may choose no skills.
An empty catalog adds no invocations and leaves handoffs unchanged. Otherwise,
fixed routes add up to two model invocations; planned routes add up to three.
There are no automatic retries or silent fallbacks. Selection, provider and
audit-write failures stop the workflow. Cancellation prevents the next main
handoff; replaying a claimed workflow cannot spend new selection calls.

### Trace the instructions delivered

Under `.koard/state/runs/<id>/skills/`, `catalog.json` contains versioned metadata,
source paths/scopes, diagnostics and the captured exact UTF-8 content. Each role
directory contains `selection-request.json`, `selection-result.json` and
`delivery.json`. The delivery record binds the main handoff ID, catalog digest,
selected IDs/digests and final Objective digest. JSON escaping does not change
the decoded source bytes. Later source edits do not change this captured run.

Audit files are write-once evidence, not signatures, review receipts or resumable
execution authority. ODD status does not independently verify these sidecars;
its existing checks validate the persisted main handoffs. A failed run can leave
partial audit evidence, and a delivery record alone does not prove execution.
Catalogs may contain private instructions: treat local run storage accordingly.
Selected source text is sent to the role's configured provider.

### Limits and trust boundaries

| Limit | Bound |
|---|---|
| Captured winning skills | 128 entries, 1 MiB aggregate source bytes |
| Individual source file | Existing 64 KiB registry limit |
| Serialized catalog metadata | 64 KiB |
| Serialized selection request | 128 KiB |
| Selected instruction suffix | 384 KiB, at most five skills |

Skills provide guidance, not capabilities. The OpenCode runtime skill loader
remains disabled; Koard delivers text through the existing prompt. Codex sandbox
flags also remain unchanged. References/assets linked by a skill are not bundled
or automatically loaded, and global source paths do not grant global read access.
The snapshot covers the skill file, not every project file a selector may inspect.
Do not treat this as isolation from malicious instructions or a concurrently
hostile filesystem. Use trusted skill sources and `--skills=off` when appropriate.

Independent review and correction do not discover or select live skills through
this integration. Tests use deterministic fake runners to prove selection
validation, exact-byte transport and unchanged runtime policy; they do not prove
that a real model selects well or follows instructions.

For contributors, start at `internal/application/skillselection`: it composes
registry capture and the provider-neutral runner. The router's optional
`Instructions` callback returns only an Objective suffix before persistence.
`internal/cli/automatic_skills_test.go` exercises both policies and entry points;
adapter prompt tests cover transport without live providers.

## What is not implemented

Skill installation, generation, settings synchronization and runtime-native
skill registration remain planned. Koard owns transitions, typed-result
validation, review consent and application authority in executable code.
