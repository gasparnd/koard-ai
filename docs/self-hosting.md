# Self-hosting Koard

Koard should eventually use its own orchestration model to develop Koard. This is progressive bootstrapping, not a claim that the current CLI already orchestrates agents.

## Current bootstrap

The repository exposes its intent through:

- `AGENTS.md`, the entry point for any contributing agent;
- `docs/`, the architecture and operational context;
- `.koard/koard.yaml`, the portable project configuration;
- `.koard/workflows/odd.md`, the workflow contract under development;
- `docs/roadmap.md`, the source of truth for implemented capabilities.

Until the workflow engine exists, humans and host runtimes enforce these documents.

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
