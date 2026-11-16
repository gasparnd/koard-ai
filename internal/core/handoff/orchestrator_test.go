package handoff

import "testing"

func TestIndependentSenderBoundary(t *testing.T) {
	r := Request{
		SchemaVersion: 1, ID: "h", TaskID: "t",
		From: Role("orchestrator"), To: RolePlanner,
		Objective: "plan", ExpectedOutput: OutputContract{Format: "json"},
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.From, r.To = RoleExecutor, Role("orchestrator")
	if err := r.Validate(); err == nil || err.Error() != `to role "orchestrator" is invalid` {
		t.Fatal(err)
	}
	result := Result{SchemaVersion: 1, HandoffID: "h", Role: Role("orchestrator"), Status: StatusCompleted, Summary: "done"}
	if err := result.Validate(); err == nil || err.Error() != `role "orchestrator" is invalid` {
		t.Fatal(err)
	}
	result.Role = RolePlanner
	result.Next = &Transition{Role: Role("orchestrator"), Reason: "invalid"}
	if err := result.Validate(); err == nil || err.Error() != `next.role "orchestrator" is invalid` {
		t.Fatal(err)
	}
}
