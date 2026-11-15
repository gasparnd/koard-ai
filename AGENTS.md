# Agent entry point

This repository is developed by humans and AI agents. Before changing code, read these documents in order:

1. `README.md` — product purpose and current capabilities.
2. `docs/architecture.md` — boundaries and dependency rules.
3. `docs/roadmap.md` — implemented and planned work.
4. `docs/contributing.md` — development and verification rules.
5. `docs/self-hosting.md` — how Koard progressively operates on itself.
6. `docs/skill-style-guide.md` — required format for project skills.

## Repository rules

- Keep the core independent from Claude, Codex, Copilot, OpenCode, and other providers.
- Add provider behavior behind an adapter interface.
- Exchange agent work through typed artifacts; do not depend on free-form model-to-model conversation.
- Keep documentation synchronized with observable CLI behavior.
- Never describe a planned command as implemented.
- Add or update public-interface tests for every CLI behavior change.
- Use English for code, identifiers, messages, and repository documentation.

## Current verification

```bash
go test ./...
go vet ./...
```
