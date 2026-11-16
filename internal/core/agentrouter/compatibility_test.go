package agentrouter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
)

type compatibilityProbe struct{}

func (compatibilityProbe) Execute(_ context.Context, request handoff.Request) (handoff.Result, error) {
	return handoff.Result{SchemaVersion: 1, HandoffID: request.ID, Role: request.To, Status: handoff.StatusCompleted, Summary: "same"}, nil
}

func TestDefaultOutcomeCompatibility(t *testing.T) {
	outcome, err := Route(context.Background(), Options{WorkflowID: "same", TaskID: "task", Objective: "goal", StateDir: t.TempDir(), Workspace: t.TempDir(), Runner: compatibilityProbe{}, Clock: func() time.Time { return time.Unix(1, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	// The original default route's deterministic wire representation.
	if digestOf(encoded) != "56eb57d9aca801b022731793a771f543d9bf8fe0b80224b95c768bfee50d3b09" {
		t.Fatalf("default outcome changed: %s", encoded)
	}
}
