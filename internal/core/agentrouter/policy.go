package agentrouter

import (
	"fmt"

	"github.com/koard-ai/koard/internal/core/handoff"
)

const (
	PolicyFixed   = "fixed"
	PolicyPlanned = "planned"
)

// PolicySteps returns a fresh, bounded route. Model results cannot change it.
// An empty policy preserves the original two-handoff behavior.
func PolicySteps(policy string) ([]Step, error) {
	steps := []Step{{From: handoff.RolePlanner, To: handoff.RoleExecutor}, {From: handoff.RoleExecutor, To: handoff.RoleVerifier}}
	switch policy {
	case "", PolicyFixed:
		return steps, nil
	case PolicyPlanned:
		return append([]Step{{From: handoff.RoleOrchestrator, To: handoff.RolePlanner}}, steps...), nil
	default:
		return nil, fmt.Errorf("unsupported routing policy %q; supported policies are %q and %q", policy, PolicyFixed, PolicyPlanned)
	}
}

func stepObjective(options Options, step Step, index int) string {
	switch {
	case step.To == handoff.RolePlanner:
		return "Plan the requested work without modifying the workspace: " + options.Objective
	case step.To == handoff.RoleExecutor && index > 0:
		return "Propose the requested change using the previous plan: " + options.Objective
	case step.To == handoff.RoleVerifier:
		return "Verify the result of the previous handoff for: " + options.Objective
	default:
		return options.Objective
	}
}
