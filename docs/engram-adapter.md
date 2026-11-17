# Engram adapter

The internal adapter implements Koard's memory store using **Engram 3.7.2**.
It is not yet connected to `koard run` or public memory-decision commands.
The roadmap's Engram integration remains incomplete until that wiring is verified.

## Owned process and storage

`engram.Start` requires explicit absolute workspace and data directories. It
starts `engram serve --session-owned` in the canonical workspace, with a fresh
authentication token and a retained stdin pipe. There is no ambient-daemon fallback.

The child environment removes inherited `ENGRAM_*` and `GIT_*` variables to avoid
service, cloud, or repository redirection. It supplies only `ENGRAM_DATA_DIR`,
`ENGRAM_TOKEN`, and `ENGRAM_CLOUD_AUTOSYNC=0` for Engram. Ordinary OS configuration
remains inherited. This disables this child's autosync, not synchronization by
other clients sharing the same storage. Startup may normalize Engram metadata.

Before returning an adapter, startup validates:

- A newline-terminated readiness frame of at most 1,024 bytes, the child PID,
  instance identity, and a literal loopback HTTP endpoint.
- Authenticated health identity, version 3.7.2, and session-registration capability.
- Active storage compatibility revision 1, matching instance, runtime identity,
  and `startup-verified-store-v1` authorization.
- An unambiguous project resolution for the workspace, followed by a fresh
  `project_owned` session with `resume:false` and an exact acknowledgement.

Startup and each HTTP request have a ten-second deadline. Redirects, proxy
discovery, and connection reuse are disabled; responses are bounded to 2 MiB.
Provider response bodies and stderr are not echoed into errors. Every request
carries the owned token. The implementation serializes store calls and shutdown.

`Close` ends only the acknowledged session, then closes child stdin and waits up
to six seconds. If necessary it kills only that retained child and waits one more
second, reporting forced or unconfirmed shutdown. Repeated `Close` calls return
the original result without another session-end request.

## Scoped memory operations

`Resolve` rechecks canonical project identity; a changed project or workspace is
refused rather than silently rebinding the session. Configuration display names
are not project identities.

`Search` always sends the canonical project and `scope=project`, admits at most
20 hits, and obtains every complete observation separately. Both hits and full
records must have matching ownership; snippets never become retained knowledge.

Engram's full observation JSON omits lifecycle state. Koard derives it from
`review_after`: an elapsed timestamp means `needs_review`. Superseded records are
also treated as needing review; malformed dates are refused conservatively.
Missing review dates mean active, matching the upstream default. Memory remains
advisory and this classification does not establish semantic truth.

`Create` makes one POST with the registered session, project, stable operation
identity, and `capture_prompt:false`. It omits topic keys and current prompts,
never PATCHes existing knowledge, and validates a full read after saving.
Engram may still update duplicate counters. Any failed or mismatched response is
an error for the coordinator's unknown-outcome policy, never an automatic retry.
Unknown-write recovery lookup is deliberately not implemented in this slice.

## Verification

`go test ./internal/adapters/engram` exercises a separate fake child through real
stdin/stdout and authenticated loopback HTTP, including identity, scope, lifecycle,
redirect, oversized-response, cancellation, and lost-acknowledgement refusals.

When Engram 3.7.2 is installed, `TestIsolatedEngram372` also starts the real binary
with temporary HOME, workspace, and data directories. It saves, fully reads,
searches, and closes one test session. It never opens the shared database.
`go test -short ./internal/adapters/engram` skips that external-binary test.

Protocol references are pinned to the [v3.7.2 server routes][routes],
[owned startup][startup], and [store compatibility][compatibility].

[routes]: https://github.com/Gentleman-Programming/engram/blob/v3.7.2/internal/server/runtime_contract.go
[startup]: https://github.com/Gentleman-Programming/engram/blob/v3.7.2/cmd/engram/serve_owned.go
[compatibility]: https://github.com/Gentleman-Programming/engram/blob/v3.7.2/internal/store/compatibility.go
