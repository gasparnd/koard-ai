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
3. Register it in `AGENTS.md` and this page until runtime projection exists.
4. Run compatible and strict validation for the project's authored skills.
5. When changing discovery, add public CLI tests in `internal/cli/skill_test.go`;
   use temporary project/home directories, never actual user skill folders.
6. Run `go test ./...` and `go vet ./...` and update this reference alongside
   any CLI or precedence change.

## What is not implemented

Discovery is not automatic selection, technology detection, runtime injection,
skill installation, generation, or settings synchronization. The current
OpenCode adapter still disables runtime skills; listing these files does not
change that policy. Future projection must preserve permissions and the exact
instructions chosen for a handoff. Koard's code continues to own transitions,
typed-result validation, review consent and application authority.
