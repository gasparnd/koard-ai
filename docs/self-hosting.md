# Self-hosting Koard

Koard can guide its own bounded proposal workflows through `koard odd run` and
`koard odd status`, including real planning with `--policy planned`. Review,
correction and apply require their existing explicit commands and decisions.
This is not autonomous development or delivery. See [odd-runtime.md](odd-runtime.md).

## Current bootstrap

`koard native setup --runtime opencode` can prepare project-local instructions
for ordinary host conversations; it does not require `odd run` for each task.
It is not installed into this checkout automatically. See the
[native guide](opencode-native.md) for conflict handling, bounded activation
and memory evidence, and remaining recovery limits. The persisted workflow path
above remains independent.

The repository exposes its intent through:

- `AGENTS.md`, the entry point for any contributing agent;
- `docs/`, the architecture and operational context;
- `.koard/koard.yaml`, the portable project configuration;
- `.koard/workflows/odd.md`, the workflow contract under development;
- `docs/roadmap.md`, the source of truth for implemented capabilities.

Humans and host runtimes still interpret business scope and enforce these documents;
the guided runtime records technical preflight and tracks progress through apply.

## Target feedback loop

```text
Human objective
  → Koard classifies work
  → planner produces a versioned plan artifact
  → executor produces a candidate and evidence
  → verifier evaluates the candidate
  → Koard records state and memory
  → human receives the outcome and retains delivery control
```

## Bootstrap safety

Koard must not silently rewrite its own governing contracts. Changes to configuration schemas, handoff protocols, workflow transitions, or skill projections require tests and explicit documentation updates. Generated files must be distinguishable from authored source.

## Agent resumption contract

An agent resuming work should be able to reconstruct the current state from committed files plus explicit runtime state. It should not need hidden conversation history. Engram will augment that context, but repository documentation remains the portable baseline.
