# Koard 0.1.0 installable release
Objective: prepare verified installable packages with MIT licensing; remote publication is deferred.
Branch: `release/0.1.0`, base `44c8cb6`. Route: inline implementation; independent verification for risk item 5. Forecast: under 400 authored lines plus unchanged license text. Checks: public version RED/GREEN, package tests, Go suite/vet, extracted host smoke and duplicate-build comparison.

## Specs
### S1 — First version
User: "Perfecto, cra una nueva rama para esto. Quiero que la primera version sea 0.1.0".
The binary reports exactly `0.1.0`; prepare macOS/Linux ARM64/AMD64 archives, checksums, provenance and installation instructions. These target choices were announced; only this host receives runtime validation.
### S2 — Distribution and license
User: "Podemos publicar la primera version isntalable?"; "Si" to preparing/publishing `v0.1.0` in `gasparnd/koard-ai`, pushing the branch/tag using existing GitHub CLI authentication; "vamos con MIT".
Use MIT for Koard, preserving separate third-party notices. Do not change module identity, runtime contracts, credentials or global configuration.
### S3 — Defer remote publication
User selected "Defer publication and retain local release preparation." after no GitHub login was found.
Prepare and commit locally; do not push, tag remotely, or publish. Record the missing login and exact remaining publication steps.

## Tasks
- T1 [S1,S2] Version, license, deterministic packages and install documentation — inline; implemented and unit-checked; commits: `3bc4839`, `b5e7ec6`.
- T2 [S1,S2,S3] Build/check packages, independent verification, local commit and handoff — inline with per-action checks and independent verifier for item 5; complete locally, publication deferred; evidence commit: `5be1ade`, closure in this documentation commit.

## Log
### L1 — Authorization
"Perfecto, cra una nueva rama para esto. Quiero que la primera version sea 0.1.0"
"Podemos publicar la primera version isntalable?"
"Si"
"vamos con MIT"
"Defer publication and retain local release preparation."

### L2 — Initial evidence
Current version is `0.1.0-dev`. No release packaging or root license exists. Native OpenCode assets are embedded in the binary. The linked YAML v4 dependency supplies Apache-2.0 LICENSE/NOTICE; the embedded Engram plugin has its own MIT license/provenance. Module identity differs from the GitHub repository, so document clone/tag/local build rather than an unverified `go install` path. Go 1.27.4 is available. GitHub CLI 2.105.0 was downloaded to owned temporary storage and its official checksum verified; the authorized login does not exist. No remote publication or credential changes occurred.

Risk: item 5 (release packaging); independent verification completed. No model calls, service startup or new provider dependency is needed for release checks.

### L3 — Implementation and ordinary checks
Version aliases failed with `0.1.0-dev` before the constant change, then all passed with literal `0.1.0`. Package tests initially failed because the implementation was absent, then passed: deterministic archive bytes, ordered entries, executable/data modes, notices, usage and existing-output preservation. Final source formatting preceded checks. `go test ./...` passed (24 packages, 4 without tests); `go vet ./...` and `git diff --check` passed. `KOARD_NATIVE_PROOF` was unset: no live models/services were launched. This coherent release-preparation unit keeps packaging, licensing, tests and docs together; copied distribution assets are not rewritten. Rollback is the version constant and these added release surfaces, without altering provider behavior. Packaging requires a clean committed tree; four-target builds and host smoke follow the implementation commit.

### L4 — Package evidence and corrected preparation failures
Initial packaging refused before output creation because Homebrew places Go's LICENSE beside `libexec`, not inside GOROOT. A layout regression test failed before the helper existed, then passed for upstream and Homebrew layouts while rejecting an unrelated parent. Commit `b5e7ec6` contains the fix; full suite/vet passed again.

From `b5e7ec6`, two offline builds produced four byte-identical archives and checksum manifests. Each archive has ten safe regular entries with normalized ownership/timestamps/modes, exact third-party notice bytes and source/toolchain provenance. Host binaries report `0.1.0` for all three aliases. Assets-only and Engram previews create nothing; initial installations succeed, identical repeats preserve bytes/modes/mtimes, and strict skill validation passes. Conflict checks refuse and preserve files for both variants. The public packager rejects an existing output directory without altering it. A smoke script initially looked for `plugin` instead of `plugins`; parent verified the actual pinned plugin and completed remaining checks. A parent rejection probe initially lacked the module cache after HOME isolation; explicit existing GOPATH fixed only that probe environment. No release artifacts were changed by those probe failures.

Artifacts remain ignored under `dist/0.1.0`, built from `b5e7ec6b38ad9dcf037915448908a3cc56d30a65`. Only Darwin ARM64 was executed; other architectures were inspected as correctly targeted Mach-O/ELF binaries. No signing, notarization, Windows, model/service integration rerun or publication is claimed.

### L5 — Independent verification and local handoff
Independent verification found no blockers for S1/S2/S3. It inspected every archive/license/checksum and ran 34 baseline/current public CLI invocations in isolated state, including the documented install guard, preview, install, repeat, strict skill validation, malformed input and conflict preservation. The baseline/current difference was the intended version string. Existing native setup tests already cover the proposed preservation scenarios; no production installer change or duplicate test suite was added. Cleanup on build failure was inspected structurally, not fault-injected. Parent spot checks and final archive checksums passed. Owned build/rebuild, verifier, test and temporary GitHub CLI directories were removed; only the final ignored `dist/0.1.0` deliverables remain. No tag, push or release was created. Future publication must authenticate GitHub CLI and use the archives' exact `BUILD.txt` commit, or rebuild and verify from a newer approved commit before tagging it.

### L6 — Review closure
The user selected "Revisar este cambio" for the high-risk release slice through `5be1ade`. Four native lenses approved it; the sole informational warning requests a future fault-injection test for cleanup after a subprocess build failure. No correction was opened. Exact acknowledgement of `review-88addda39f20a3bc` returned `authority: burned`. The local preparation is complete, not remotely published; the user's publication deferral remains controlling. Tests/builds were not repeated for this passive status-only closure. Final packages still identify their actual build source `b5e7ec6`, not the later evidence-only commits.
