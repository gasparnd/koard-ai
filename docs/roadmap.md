# Roadmap

Koard grows through executable vertical slices. Planned functionality is documented separately from current functionality so users and agents do not confuse intent with evidence.

## Bootstrap — in progress

- [x] Go module and CLI entry point.
- [x] `koard init` project structure generation.
- [x] `koard doctor` configuration presence check.
- [x] Human and agent documentation entry points.
- [x] Initial self-hosting configuration.
- [ ] Run the Go test suite in an environment with Go 1.23 or newer.

## Foundation

- [ ] Versioned configuration parser and validator.
- [ ] Provider-independent agent and handoff contracts.
- [ ] ODD workflow state machine.
- [ ] Structured event and diagnostic output.

## First working orchestration

- [ ] OpenCode runtime adapter.
- [ ] Codex runtime adapter.
- [ ] Planner, executor, and verifier role routing.
- [ ] Engram memory adapter.
- [ ] End-to-end workflow with persisted handoffs.

## Portable skills

- [ ] Skill registry and validation.
- [ ] `koard skill create` generator.
- [ ] Runtime-specific skill projection.
- [ ] `koard sync` for generated settings.

## Extended portability

- [ ] Claude adapter.
- [ ] Copilot adapter.
- [ ] Capability negotiation and model selection policies.
- [ ] Migration and compatibility tooling.
