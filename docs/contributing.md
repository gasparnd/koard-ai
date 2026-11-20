# Contributing

Contributions should preserve portability and keep behavior understandable without access to one specific AI provider.

## Development loop

1. Read the relevant architecture and roadmap sections.
2. Add a failing public-interface test when deterministic testing is possible.
3. Implement the smallest coherent behavior.
4. Run focused tests, then the complete suite.
5. Update documentation that describes the changed behavior.

## Required checks

```bash
go test ./...
go vet ./...
```

## Change boundaries

- Core contracts must remain provider-neutral.
- Adapters must translate provider behavior into Koard contracts.
- CLI output is a user-facing contract and requires tests.
- Generated files must identify their source and remain reproducible.
- A skill must follow `docs/skill-style-guide.md` and be registered before use.
- For skill changes, run `go run ./cmd/koard skill validate --strict`. Document
  each skill's purpose, inputs, outputs, limits and examples for new contributors;
  [the registry guide](skills.md) explains the full workflow and compatibility rules.

## Pull requests

Explain the outcome, architectural impact, verification performed, and any capability intentionally left for a later milestone. Do not mix unrelated provider adapters in one change.
