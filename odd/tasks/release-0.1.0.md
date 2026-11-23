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
- T1 [S1,S2] Version, license, deterministic packages and install documentation — inline; implemented and unit-checked; commit: this work unit.
- T2 [S1,S2,S3] Build/check packages, independent verification, local commit and handoff — inline with per-action checks and independent verifier for item 5; suite/vet passed, artifact checks pending.

## Log
### L1 — Authorization
"Perfecto, cra una nueva rama para esto. Quiero que la primera version sea 0.1.0"
"Podemos publicar la primera version isntalable?"
"Si"
"vamos con MIT"
"Defer publication and retain local release preparation."

### L2 — Initial evidence
Current version is `0.1.0-dev`. No release packaging or root license exists. Native OpenCode assets are embedded in the binary. The linked YAML v4 dependency supplies Apache-2.0 LICENSE/NOTICE; the embedded Engram plugin has its own MIT license/provenance. Module identity differs from the GitHub repository, so document clone/tag/local build rather than an unverified `go install` path. Go 1.27.4 is available. GitHub CLI 2.105.0 was downloaded to owned temporary storage and its official checksum verified; the authorized login does not exist. No remote publication or credential changes occurred.

Risk: item 5 (release packaging); independent verification required. No model calls, service startup or new provider dependency is needed for release checks.

### L3 — Implementation and ordinary checks
Version aliases failed with `0.1.0-dev` before the constant change, then all passed with literal `0.1.0`. Package tests initially failed because the implementation was absent, then passed: deterministic archive bytes, ordered entries, executable/data modes, notices, usage and existing-output preservation. Final source formatting preceded checks. `go test ./...` passed (24 packages, 4 without tests); `go vet ./...` and `git diff --check` passed. `KOARD_NATIVE_PROOF` was unset: no live models/services were launched. This coherent release-preparation unit keeps packaging, licensing, tests and docs together; copied distribution assets are not rewritten. Rollback is the version constant and these added release surfaces, without altering provider behavior. Packaging requires a clean committed tree; four-target builds and host smoke follow the implementation commit.
