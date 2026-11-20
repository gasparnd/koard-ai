package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
)

func TestProposalSuppliesResultContract(t *testing.T) {
	for _, role := range []handoff.Role{handoff.RolePlanner, handoff.RoleExecutor, handoff.RoleVerifier} {
		t.Run(string(role), func(t *testing.T) {
			request := validRequest()
			request.From, request.To = handoff.RoleOrchestrator, role
			runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON(request.ID, role))
			got, err := testAdapter(t, runner).Propose(context.Background(), request)
			if err != nil || got.HandoffID != request.ID || got.Role != role || got.Status != handoff.StatusCompleted {
				t.Fatalf("result=%+v error=%v", got, err)
			}
			encoded, err := handoff.MarshalRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			assertPromptContains(t, runner, []string{
				"schema_version (1)", "handoff_id (copy request.id)", "role (copy request.to)",
				"status (completed, blocked, or failed)", "summary (nonblank string)",
				"artifacts", "evidence", "kind, uri, and optional digest", "next", "role and reason",
				"memory_proposals", "completed verifier", "request.context", "sensitivity", "uncertainty",
				"No additional fields", "Do not fetch the schema URI", "Handoff request:\n" + string(encoded),
			})
		})
	}
}

func TestReviewSuppliesResultContract(t *testing.T) {
	request, dir := validReviewRequest(t)
	response := review.Result{SchemaVersion: 1, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved}
	runner := promptRunner(t, "codex-cli 0.149.0", mustMarshalReviewResult(t, response))
	got, err := testAdapter(t, runner).Review(context.Background(), request, dir)
	if err != nil || got.RequestID != request.ID || got.Attempt != request.Attempt || got.Verdict != review.VerdictApproved {
		t.Fatalf("result=%+v error=%v", got, err)
	}
	assertPromptContains(t, runner, []string{
		"schema_version (1)", "request_id (copy request.id)", "attempt (copy request.attempt)",
		"target (copy the entire request.target object unchanged)", "verdict (approved, correction_required, or rejected)",
		"findings", "id, severity (warning or blocking), summary", "uri and digest", "supplied snapshot",
		"correction_required requires at least one finding", "approved must not contain blocking findings", "No additional fields",
	})
	prompt := runner.commands[1].Args[len(runner.commands[1].Args)-1]
	_, body, ok := strings.Cut(prompt, "Review request:\n")
	var sent review.Request
	if !ok || json.Unmarshal([]byte(body), &sent) != nil || sent.ID != request.ID || sent.Attempt != request.Attempt {
		t.Fatal("request not preserved in prompt")
	}
}

func assertPromptContains(t *testing.T, runner *fakeRunner, fragments []string) {
	t.Helper()
	if len(runner.commands) != 2 {
		t.Fatalf("unexpected invocation count: %d", len(runner.commands))
	}
	args := runner.commands[1].Args
	prompt := args[len(args)-1]
	for _, fragment := range fragments {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("missing contract instruction %q", fragment)
		}
	}
}
