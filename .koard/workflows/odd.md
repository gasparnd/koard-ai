# ODD workflow contract

Koard's Organic Driven Development workflow follows six active states:

1. **Authorize** — determine whether the objective permits mutation.
2. **Explore** — inspect the existing project and constraints.
3. **Resolve** — surface consequential uncertainty without inventing requirements.
4. **Classify** — select proportional tracking, execution, and verification.
5. **Implement** — execute bounded work through explicit role handoffs.
6. **Close** — report outcomes, evidence, risk, and pending decisions.

The versioned state machine in `internal/core/odd` enforces this order. `completed` is the successful terminal state. Any active state may move to `blocked`, retain its exact resume state, and later resume. `stopped` is terminal and represents an explicit stop rather than successful completion.

The state machine controls legal transitions only. Structured event persistence, diagnostics, handoff execution, and runtime adapters are separate roadmap capabilities.
