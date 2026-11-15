# Review mode

Review mode is the user's switch for receipt-driven development (RDD). It decides whether RDD applies. It does not grant consent for any candidate and never authorizes delivery.

Candidate-bound consent consumes the effective mode: new grant and decline decisions are refused while review mode is off. Consent status remains readable. Independent review and later RDD stages remain planned in `roadmap.md`.

## Commands

```bash
koard review mode status
koard review mode enable [--scope clone|global]
koard review mode disable [--scope clone|global]
```

`status` only reads. It prints the effective mode, the source that decided it, and each scope:

```text
Review mode: off (decided by global)
  global: off
  clone: on
```

`enable` and `disable` write one scope and print the resulting status. The scope defaults to `clone`, so changing every project on the machine needs an explicit `--scope global`. When the requested mode is not the effective one, the output names the scope that keeps it, for example `The global scope keeps review mode off.`

## Resolution

Off wins. The effective mode is decided in this order:

1. a clone `off`;
2. a global `off`;
3. a clone `on`;
4. a global `on`;
5. otherwise `on`, decided by `default`.

A clone `on` therefore cannot override a global `off`, and a global `on` cannot override a clone `off`.

## Setting files

| Scope | Location |
|---|---|
| `clone` | `.koard/state/review-mode.json` in the current directory, which must be a Koard project for writes |
| `global` | `review-mode.json` in `KOARD_CONFIG_DIR` when set, otherwise in the `koard` directory under the operating system's user configuration directory |

`KOARD_CONFIG_DIR` must be an absolute path. `.koard/state/` is ignored by git, so the clone setting never travels with the repository.

Each file holds one JSON object:

```json
{"schema_version":1,"mode":"off"}
```

Files are replaced atomically. A file with unknown fields, an unsupported schema version, a mode other than `on` or `off`, or trailing data fails the command with exit code `1` and names the file; it is never read as `on`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | A setting file could not be read or written, `KOARD_CONFIG_DIR` is relative, or clone scope was used outside a Koard project |
| `2` | Usage error: missing or unknown action, unknown scope, or arguments passed to `status` |
