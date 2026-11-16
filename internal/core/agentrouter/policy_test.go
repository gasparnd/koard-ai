package agentrouter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestPolicyRoutes(t *testing.T) {
	for _, policy := range []string{"", "fixed", "planned"} {
		t.Run(policy, func(t *testing.T) {
			runner := &fakeRunner{overrideResult: func(result *handoff.Result) {
				result.Next = &handoff.Transition{Role: handoff.RolePlanner, Reason: "Untrusted suggestion"}
			}}
			options := testOptions(t, runner)
			options.Policy = policy
			outcome, err := Route(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			roles := []handoff.Role{handoff.RoleExecutor, handoff.RoleVerifier}
			objectives := []string{options.Objective, "Verify the result of the previous handoff for: " + options.Objective}
			if policy == "planned" {
				roles = append([]handoff.Role{handoff.RolePlanner}, roles...)
				objectives = []string{"Plan the requested work without modifying the workspace: " + options.Objective, "Propose the requested change using the previous plan: " + options.Objective, objectives[1]}
			}
			if len(outcome.Handoffs) != len(roles) {
				t.Fatalf("handoffs: %#v", outcome.Handoffs)
			}
			dir := filepath.Join(options.StateDir, options.WorkflowID)
			for i, request := range outcome.Handoffs {
				if request.To != roles[i] || request.Objective != objectives[i] || !reflect.DeepEqual(request.Capabilities, []string{"repository:read"}) {
					t.Fatalf("request %d: %#v", i, request)
				}
				if i > 0 {
					data := readFile(t, filepath.Join(dir, "handoffs", resultFile(i)))
					if request.Context[0].Digest != "sha256:"+digestOf(data) {
						t.Fatal("lost previous result binding")
					}
				}
			}
			if policy == "planned" && outcome.Handoffs[0].From != handoff.Role("orchestrator") {
				t.Fatal("planner has no truthful origin")
			}
			if outcome.Candidate == nil || outcome.Risk == nil || outcome.Status != handoff.StatusCompleted {
				t.Fatalf("outcome: %#v", outcome)
			}
			if outcome.Candidate.HandoffID != outcome.Handoffs[len(roles)-2].ID {
				t.Fatal("candidate did not come from executor")
			}
			persisted := readOutcome(t, filepath.Join(dir, "workflow.json"))
			if !reflect.DeepEqual(outcome, persisted) {
				t.Fatal("persisted outcome differs")
			}
			if len(readRecords(t, filepath.Join(dir, "events.jsonl"))) != 2*len(roles) {
				t.Fatal("missing events")
			}
		})
	}
}

func TestPlannedRouteStopsAtPlanner(t *testing.T) {
	for _, status := range []handoff.Status{handoff.StatusBlocked, handoff.StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			runner := &fakeRunner{statuses: []handoff.Status{status}}
			options := testOptions(t, runner)
			options.Policy = "planned"
			outcome, err := Route(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			if len(runner.requests) != 1 || outcome.Candidate != nil || outcome.Risk != nil || outcome.Status != status {
				t.Fatalf("outcome: %#v", outcome)
			}
			if got := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json")); got.Status != status {
				t.Fatal("wrong persisted status")
			}
		})
	}
}

func TestUnknownPolicyDoesNotClaimWorkflow(t *testing.T) {
	runner := &fakeRunner{}
	options := testOptions(t, runner)
	options.Policy = "automatic"
	if _, err := Route(context.Background(), options); err == nil || !strings.Contains(err.Error(), "unsupported routing policy") {
		t.Fatalf("error: %v", err)
	}
	entries, err := os.ReadDir(options.StateDir)
	if err != nil || len(entries) != 0 || len(runner.requests) != 0 {
		t.Fatal("invalid policy created state or ran provider")
	}
}

func TestOrchestratorIsSenderOnly(t *testing.T) {
	request := handoff.Request{SchemaVersion: 1, ID: "plan", TaskID: "task", From: handoff.Role("orchestrator"), To: handoff.RolePlanner, Objective: "Plan", ExpectedOutput: handoff.OutputContract{Format: "application/json"}}
	if _, err := handoff.MarshalRequest(request); err != nil {
		t.Fatal(err)
	}
	request.From, request.To = handoff.RolePlanner, handoff.Role("orchestrator")
	if _, err := handoff.MarshalRequest(request); err == nil {
		t.Fatal("orchestrator accepted as model destination")
	}
	result := handoff.Result{SchemaVersion: 1, HandoffID: "plan", Role: handoff.Role("orchestrator"), Status: handoff.StatusCompleted, Summary: "Plan"}
	if _, err := handoff.MarshalResult(result); err == nil {
		t.Fatal("orchestrator accepted as responding model")
	}
	result.Role = handoff.RolePlanner
	result.Next = &handoff.Transition{Role: handoff.Role("orchestrator"), Reason: "Escalate"}
	if _, err := handoff.MarshalResult(result); err == nil {
		t.Fatal("model may not request orchestrator authority")
	}
}
