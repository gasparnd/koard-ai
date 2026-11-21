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

## Branching

Every roadmap step has one integration branch named `step/<number>-<slug>`, and every task inside that step has its own branch named `task/<step>.<task>-<slug>`.

```text
develop
 └── step/1-non-mutating-walking-skeleton     pull request → develop (umbrella, draft)
      ├── task/1.6-cancellation               pull request → step/1 (one task)
      └── task/1.7-next-slice                 pull request → step/1 (one task)
```

- Create each task branch from the current tip of its step branch, and open its pull request against the step branch. That keeps the reviewed diff limited to one task while the step branch accumulates the work.
- Keep the step pull request in draft until the step is complete. It is the umbrella review for the whole step, and it targets `develop`.
- Do not open a task pull request against `develop`. A step that lands task by task without its umbrella loses the step-level review it exists for.
- State the review budget of every task pull request, counted as additions plus deletions. A step umbrella exceeds the usual budget by construction; say so there instead of compressing the change.

