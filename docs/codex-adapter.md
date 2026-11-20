# Codex runtime adapter

`internal/adapters/codex` is Koard's second runtime adapter. It translates one provider-neutral handoff request — or one independent-review request — into a single non-interactive Codex CLI run, and accepts back exactly one strict JSON result bound to the request it was given.

## Why a second adapter

For the shared design, debugging stages, and new-runtime checklist, see
[Understand, debug, and extend runtime adapters](adapters.md).

The core decides *what* role must act and *what evidence* it must return. Providers decide *how* that request is performed. Until this milestone exactly one provider could perform it, which made "provider-neutral" a claim the repository could not yet demonstrate. A second adapter implemented against the same contracts — and selected by configuration rather than by code — is the evidence.

## The guard is the OS sandbox, not a per-tool policy

The OpenCode adapter denies every tool and then re-allows `read`, `glob`, and `grep`. That works because OpenCode evaluates that policy itself. Codex has no equivalent per-tool allow-list, so its adapter uses the guarantee Codex actually offers: an OS-level read-only sandbox (`-s read-only`) where a write attempt fails inside the sandbox instead of being offered as an approval decision.

That difference is the point worth stating plainly. The two adapters reach the same *effect* — a run that cannot change the workspace — through two different mechanisms, and neither mechanism makes the other's assumptions.

## Invocation surface

Every invocation is `codex exec` with exactly this surface:

| Flag | Why it is there |
|---|---|
| `--ephemeral` | The run persists no session, so a later attempt can never resume it. Reviewer independence depends on this. |
| `--ignore-user-config` | A user-level `config.toml` could redefine the sandbox, enable hooks, or add MCP servers. Ignoring it means the guard is decided here, not by ambient user state. Authentication is unaffected — Codex still reads `CODEX_HOME` for credentials, which a real logged-in run confirmed. |
| `--ignore-rules` | Execpolicy rule files can widen what may run. Same reasoning. |
| `--skip-git-repo-check` | A review runs inside a materialized snapshot directory, which is deliberately not a git repository. |
| `-s read-only` | The sandbox itself. |
| `-C <dir>` | The workspace, or the materialized review snapshot — never anything else. |
| `-m <model>` | The destination role's configured model for handoffs; `agents.verifier.model` for review/correction. |
| `--color never` | Keeps operator-facing errors free of escape sequences. |
| `--output-last-message <file>` | Where the result is read from, staged in a temporary directory outside the reviewed tree. |

The adapter never passes `--dangerously-bypass-approvals-and-sandbox`, `--approve-for-me`, `--add-dir`, `workspace-write`, `danger-full-access`, `resume`, `fork`, or an image attachment. Tests assert both directions: the required surface is present and the forbidden surface is absent.

## Why the result comes from a file

Codex prints progress to stdout alongside the answer, so a decoder pointed at the stream would have to guess which line is the result. Instead the adapter stages a temporary directory, passes `--output-last-message`, and reads exactly that file. The read is strict: unknown fields are rejected, a second JSON value is rejected, and an absent or blank file is an error rather than an empty result.

The staged directory is created outside the workspace and removed when the invocation returns.

Both prompts explicitly describe Koard's result fields rather than assuming a
schema URI is enough. Handoffs bind `handoff_id` and `role` to the request; reviews
copy the request identity, attempt and entire target. Optional nested fields and
allowed statuses/verdicts are described, and additional fields are prohibited.
Core contracts, strict decoders and safety flags remain unchanged. A schema-valid
answer still needs its binding and evidence checks; prompt instructions alone
are not proof of compliance.

## Version gate

The adapter refuses anything older than `0.149.0`, the release whose flag surface is verified here: `exec`, `-s read-only`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, and `--output-last-message`. An older CLI might accept a subtly different invocation, and an adapter that silently degrades its own guard is worse than one that refuses to start. An unparsable `--version` output is refused for the same reason.

## Review runs on materialized bytes

`Review` reuses `snapshot.Materialize` to reconstruct the exact reviewed snapshot as an ordinary file tree in a disposable directory, verifying each entry's digest as it writes, and runs Codex there with `-C`. The live workspace — which may have drifted since consent was recorded — is never inspected. The directory is removed as soon as the invocation returns.

## Provider selection

`koard review run` builds its reviewer and corrector through one factory in `internal/cli` that reads `agents.verifier.provider`:

```yaml
agents:
  verifier:
    provider: codex   # or: opencode
    model: gpt-5-codex
```

Any other value — including the bootstrap `unset` that `koard init` writes, which records that role selection is still unresolved — fails closed with a message naming both supported providers. It never silently falls back to one of them: a project that did not choose a runtime for review should be told so, not given whichever adapter happens to be first in a switch.

`ReviewerIdentity.Runtime` records the selected provider in the review request and lifecycle, so persisted evidence names which runtime actually produced a verdict.

## Limits, stated plainly

- **Project-level Codex configuration is still read.** Only user configuration and rule files are ignored; a `.codex/` directory inside the repository can still influence the run. The adapter's guarantee covers the tool calls it drives — Codex's own shell tool stays inside the read-only sandbox, and a write attempt fails there (verified against a real `0.149.0` run: *"writing is blocked by read-only sandbox"*). It does not claim that every process Codex itself starts, such as a project-configured MCP server, is contained, because the adapter has no flag that ignores project configuration.
- **Approvals are not configured, they are absent.** The adapter passes no approval flags, and `codex exec` is non-interactive, so anything that would need approval fails instead of being granted. The adapter never enables auto-approval.
- **Network isolation is not asserted.** The adapter does not verify or configure network access; its guarantee is about workspace mutation, and claiming more than that would be a claim it cannot test.
- **Routing stays outside the adapter.** `koard run` composes this adapter per destination role; independent review remains a separate command and authority boundary. See [routing.md](routing.md).

## Verification

```bash
go test ./internal/adapters/codex
go test ./internal/cli -run TestReviewProviders
go test ./...
go vet ./...
```

A real end-to-end invocation additionally requires a logged-in Codex CLI:

```bash
codex login status
```

The adapter's own tests never spawn a real process: they use a fake runner that answers the version probe and writes the exact `--output-last-message` file a real Codex process would write.
