# Use Claude Code with prepared context and no tools

Select `provider: claude` when you want Claude to evaluate material that Koard
already has, using Claude Code's native subscription login. Koard launches a
fresh process with **no model tools**, sends a bounded evidence bundle on stdin,
and validates the returned JSON. It does not give Claude the project directory.

This is useful as a verifier after another runtime has inspected the project,
or as an independent reviewer of a captured candidate. It is not an autonomous
repository explorer, editor, test runner, or operating-system sandbox.

## Quick path

1. Install Claude Code **stable 2.x, version 2.1.316 or newer** yourself. Sign in
   through the unmodified Claude Code CLI using your own subscription. Koard
   does not install Claude, start login, read tokens, or configure billing.
2. Edit the existing role configuration, for example:

   ```yaml
   agents:
     planner:
       provider: codex
       model: <your-codex-model>
     executor:
       provider: codex
       model: <your-codex-model>
     verifier:
       provider: claude
       model: sonnet
   ```

   Replace the Codex placeholders with models available to you. This is an
   `agents` section, not a replacement for the complete configuration file.
   `sonnet` is resolved by Claude Code and your account, not by Koard.
3. When you are ready to authorize real provider usage, run:

   ```sh
   koard run --workflow payment-check --task payments \
     --objective "Inspect the payment flow and cite the source files used" \
     --skills=off
   ```

   The fixed route runs the configured executor, then Claude as verifier. The
   planner only runs with `--policy planned`. `--skills=off` avoids optional
   selection calls in this first example; it does not change tool permissions.

These commands use your provider accounts and consume their usage allowances.
Koard development tests use fake processes and do not execute this example with
real models. `koard odd run` supports the same provider configuration. Independent
review remains a separate [consent-bound operation](independent-review.md).

## Example: what Claude actually receives

Suppose the executor returns a typed result referencing `repo://payments.go`.
Koard reads that explicitly referenced file using its existing containment and
digest checks, then sends Claude:

- the exact handoff objective, role, identity and result contract;
- the previous typed result;
- the referenced source text, URI and SHA-256 digest.

Claude can compare the supplied evidence and return observations. It cannot
open `users.go`, browse the web, run tests, edit files, or invoke another agent.
If `users.go` is needed but absent, the prompt requires a `blocked` result naming
the missing context. A completed response never proves that tests ran.

An objective mentioning a filename is **not** a file-selection mechanism.
The current CLI does not automatically attach source to the first handoff.
An all-Claude route can therefore lack the evidence necessary to complete a
repository task. Start with the mixed-runtime example, or supply typed context
through the runner interface. A self-contained planning question can be answered
from its objective alone, but the adapter cannot prove semantic sufficiency.

## Context and failure rules

| Input | Delivery |
|---|---|
| Direct handoff context | Explicit `repo://` references, checked against declared digests |
| Previous handoff result | Its typed artifacts and evidence expanded exactly one hop |
| Skill selection/instructions | Already embedded objective text; no skill-file discovery |
| Independent review | Only the target-bound snapshot bytes, never the live files |
| Correction | Existing digest-bound references; live drift is refused, not repaired |

There is no repository walk, recursive link following, Markdown reference
expansion, inferred filename loading, or automatic global context. Sources are
deduplicated by URI, and contradictory digests fail. Up to 64 text files and a
512 KiB serialized prompt (including request and JSON escaping) are accepted.
Missing, escaping, digest-mismatched, binary/NUL or oversized evidence fails
before Claude starts. Nothing is silently truncated.

Credential-like paths such as `.env`, `.ssh`, `.aws`, `.git`, credential files,
and private-key files are refused, including resolved source aliases. This is
defense in depth, **not a secret detector**: ordinary source and objectives can
still contain sensitive text. Select and review what you authorize sending.
Explicit model input goes to the provider; a tool-free run is not an offline run.

## Login and containment

Every invocation uses an empty temporary directory and an explicit system
prompt. Koard requests safe/restricted modes, no tools, `dontAsk`, no session
persistence, empty user/project setting sources, disabled hooks/auto-memory,
disabled slash commands/Chrome, and an empty strict MCP configuration.
It never adds permission bypass, resume, extra directories or tool-enabled retry.

`--bare` is deliberately absent: it disables OAuth/keychain authentication.
Koard removes inherited `ANTHROPIC_*` and `CLAUDE_*` overrides except
`CLAUDE_CONFIG_DIR`, retaining the native login location. It requests
`forceLoginMethod: claudeai` and checks native `auth status --json`; only
`authMethod: claude.ai` passes. API keys, token overrides, helpers or third-party
methods reported by that check are refused, with no API fallback or key prompt.

The auth check is a runtime-reported precondition, not an atomic entitlement or
billing guarantee. Saved authentication and administrator policy remain owned by
Claude Code. Use a trusted installation/account configuration; Koard does not
inspect credentials or override organizational controls.

**No OS sandbox is created.** Tool exclusion controls the model, not every action
of the runtime process. Managed policy hooks and built-in runtime behavior may
remain active, and the runtime may write its own authentication or operational
state. An empty cwd does not confine a hostile process. If policy prevents the
restricted invocation, Koard fails rather than weakening it. No claim is made
that this mode prevents all subprocess side effects or all network traffic.

## Results and troubleshooting

Claude's JSON envelope must be one `result`/`success` object with an explicit
`is_error: false` and a nonempty final `result` string. That string must pass
Koard's strict handoff or review decoder; handoff identity and role remain bound
to the request. The review core owns target, findings and independence checks.
No fences, extra JSON, progress fallback, repair or automatic retries are used.
Captured stdout is limited to 1 MiB; raw stdout/stderr is not put into errors.

| Failure | Action |
|---|---|
| Version refused | Check the installed stable CLI; do not remove restrictions |
| Native login required | Sign in through Claude Code with your subscription |
| Missing or changed evidence | Correct the explicit references or start a fresh workflow |
| Context too large/binary | Select a smaller text-only task; no automatic truncation |
| Claude returns `blocked` | Supply the missing evidence through an appropriate upstream role |
| Process or envelope failure | Check native installation, account and managed policy separately |

## Contributor map and proof limits

`internal/adapters/claude` owns context admission, native process flags, auth
metadata checks and transport decoding. It reuses `snapshot.ReadReference`,
`snapshot.Materialize`, the capability guard and handoff validators. CLI factories
select the adapter; the core imports no providers. No new config schema, skill
projection target or sync runtime is added.

Deterministic tests cover roles, packaging, frozen review, refusals, fake native
process environment/cwd, cancellation, cleanup, strict results and CLI composition.
Run `go test ./...` and `go vet ./...`. Real subscription authentication, model
compliance, managed installations and Windows execution need separate authorized
validation; the local tests do not establish them.

The flag/auth contract is grounded in the official [CLI reference](https://code.claude.com/docs/en/cli-reference),
[headless guide](https://code.claude.com/docs/en/headless),
[authentication guide](https://code.claude.com/docs/en/authentication), and
[changelog](https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md).
The minimum includes the documented restricted/settings fixes through 2.1.316;
it is not a claim that every later version has been tested. Native login stays
inside the unmodified binary; Koard is not a credential proxy or subscription
reseller. Applicable [provider terms](https://code.claude.com/docs/en/legal-and-compliance)
still govern usage.
