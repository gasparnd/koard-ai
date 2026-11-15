package agentrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/eventstream"
	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestRouteRunsFixedRouteAndBindsResults(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	outcome, err := Route(context.Background(), testOptions(t, runner))
	if err != nil {
		t.Fatalf("route workflow: %v", err)
	}
	if outcome.Status != handoff.StatusCompleted {
		t.Fatalf("expected completed outcome, got %q", outcome.Status)
	}
	if len(outcome.Handoffs) != 2 || len(outcome.Results) != 2 {
		t.Fatalf("expected two handoffs and results, got %d and %d", len(outcome.Handoffs), len(outcome.Results))
	}
	if len(runner.requests) != 2 {
		t.Fatalf("expected the runner to see two requests, got %d", len(runner.requests))
	}

	expected := [][2]handoff.Role{
		{handoff.RolePlanner, handoff.RoleExecutor},
		{handoff.RoleExecutor, handoff.RoleVerifier},
	}
	for index, roles := range expected {
		if outcome.Handoffs[index].From != roles[0] || outcome.Handoffs[index].To != roles[1] {
			t.Fatalf("step %d route is %s -> %s, expected %s -> %s", index, outcome.Handoffs[index].From, outcome.Handoffs[index].To, roles[0], roles[1])
		}
		if runner.requests[index].ID != outcome.Handoffs[index].ID {
			t.Fatalf("runner request %d is not the routed request", index)
		}
		if outcome.Results[index].HandoffID != outcome.Handoffs[index].ID {
			t.Fatalf("result %d is not bound to its request", index)
		}
	}
	if outcome.Handoffs[0].ID == outcome.Handoffs[1].ID {
		t.Fatal("handoff identities must be distinct")
	}
}

func TestRoutePersistsHandoffsAndEvents(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	options := testOptions(t, runner)
	outcome, err := Route(context.Background(), options)
	if err != nil {
		t.Fatalf("route workflow: %v", err)
	}

	workflowDir := filepath.Join(options.StateDir, options.WorkflowID)
	persisted := readOutcome(t, filepath.Join(workflowDir, "workflow.json"))
	if persisted.WorkflowID != outcome.WorkflowID || persisted.Status != outcome.Status {
		t.Fatalf("persisted outcome does not match: %#v", persisted)
	}

	for index, request := range outcome.Handoffs {
		path := filepath.Join(workflowDir, "handoffs", requestFile(index+1))
		decoded, err := handoff.DecodeRequest(openFile(t, path))
		if err != nil {
			t.Fatalf("decode persisted request %d: %v", index+1, err)
		}
		if decoded.ID != request.ID {
			t.Fatalf("persisted request %d identity changed", index+1)
		}

		path = filepath.Join(workflowDir, "handoffs", resultFile(index+1))
		result, err := handoff.DecodeResult(openFile(t, path))
		if err != nil {
			t.Fatalf("decode persisted result %d: %v", index+1, err)
		}
		if result.HandoffID != request.ID {
			t.Fatalf("persisted result %d is not bound to its request", index+1)
		}
	}

	records := readRecords(t, filepath.Join(workflowDir, "events.jsonl"))
	if len(records) != 2*len(outcome.Handoffs) {
		t.Fatalf("expected %d event records, got %d", 2*len(outcome.Handoffs), len(records))
	}
	for index, record := range records {
		if record.Sequence != uint64(index+1) {
			t.Fatalf("record %d sequence is %d, expected %d", index, record.Sequence, index+1)
		}
		if record.WorkflowID != options.WorkflowID {
			t.Fatalf("record %d workflow identity changed", index)
		}
		if record.Event == nil || record.Event.HandoffID == "" {
			t.Fatalf("record %d does not bind a handoff", index)
		}
	}
	if records[0].Event.Name != "handoff.started" || records[1].Event.Name != "handoff.completed" {
		t.Fatalf("unexpected event names: %q and %q", records[0].Event.Name, records[1].Event.Name)
	}
}

func TestRouteStopsOnNonCompletedResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{statuses: []handoff.Status{handoff.StatusBlocked}}
	options := testOptions(t, runner)

	outcome, err := Route(context.Background(), options)
	if err != nil {
		t.Fatalf("route blocked workflow: %v", err)
	}
	if outcome.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked outcome, got %q", outcome.Status)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("expected the route to stop after the blocked result, got %d requests", len(runner.requests))
	}
	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Status != handoff.StatusBlocked {
		t.Fatalf("persisted status is %q, expected blocked", persisted.Status)
	}
}

func TestRouteRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Options)
		expected string
	}{
		{name: "missing workflow identity", mutate: func(options *Options) { options.WorkflowID = "" }, expected: "workflow_id is required"},
		{name: "missing task identity", mutate: func(options *Options) { options.TaskID = "" }, expected: "task_id is required"},
		{name: "missing objective", mutate: func(options *Options) { options.Objective = "" }, expected: "objective is required"},
		{name: "missing state directory", mutate: func(options *Options) { options.StateDir = "" }, expected: "state directory is required"},
		{name: "missing runner", mutate: func(options *Options) { options.Runner = nil }, expected: "runner is required"},
		{name: "path traversal", mutate: func(options *Options) { options.WorkflowID = "../escape" }, expected: "single path segment"},
		{name: "nested path", mutate: func(options *Options) { options.WorkflowID = "nested/workflow" }, expected: "single path segment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options := testOptions(t, &fakeRunner{})
			test.mutate(&options)
			if _, err := Route(context.Background(), options); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestRouteRejectsExistingWorkflow(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	options := testOptions(t, runner)
	workflowDir := filepath.Join(options.StateDir, options.WorkflowID)
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("prepare workflow directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "workflow.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("prepare outcome: %v", err)
	}

	if _, err := Route(context.Background(), options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing workflow error, got %v", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("existing workflow still invoked the runner %d times", len(runner.requests))
	}
}

func TestRouteRejectsUnboundResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{overrideResult: func(result *handoff.Result) { result.Role = handoff.RolePlanner }}
	options := testOptions(t, runner)

	if _, err := Route(context.Background(), options); err == nil || !strings.Contains(err.Error(), "does not match request destination") {
		t.Fatalf("expected binding error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(options.StateDir, options.WorkflowID, "workflow.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unbound result must not persist a completed outcome, got %v", err)
	}
}

func TestRoutePropagatesRunnerFailure(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{failure: errors.New("provider unavailable")}
	if _, err := Route(context.Background(), testOptions(t, runner)); err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected runner failure, got %v", err)
	}
}

func TestRouteSecondObjectiveReferencesOriginal(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	options := testOptions(t, runner)
	if _, err := Route(context.Background(), options); err != nil {
		t.Fatalf("route workflow: %v", err)
	}
	if !strings.Contains(runner.requests[1].Objective, options.Objective) {
		t.Fatalf("second objective %q does not reference %q", runner.requests[1].Objective, options.Objective)
	}
	if runner.requests[1].TaskID != options.TaskID {
		t.Fatalf("second request lost the task identity: %q", runner.requests[1].TaskID)
	}
}

func testOptions(t *testing.T, runner Runner) Options {
	t.Helper()

	return Options{
		WorkflowID: "workflow-1",
		TaskID:     "task-1",
		Objective:  "Propose the requested change without modifying the workspace.",
		StateDir:   t.TempDir(),
		Runner:     runner,
		Clock:      func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) },
	}
}

func readOutcome(t *testing.T, path string) Outcome {
	t.Helper()

	file := openFile(t, path)
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var outcome Outcome
	if err := decoder.Decode(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	return outcome
}

func readRecords(t *testing.T, path string) []eventstream.Record {
	t.Helper()

	decoder := eventstream.NewDecoder(openFile(t, path))
	var records []eventstream.Record
	for {
		record, err := decoder.Next()
		if err != nil {
			break
		}
		records = append(records, record)
	}
	return records
}

func openFile(t *testing.T, path string) *os.File {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func requestFile(step int) string {
	return fmt.Sprintf("%d-request.json", step)
}

func resultFile(step int) string {
	return fmt.Sprintf("%d-result.json", step)
}

type fakeRunner struct {
	requests       []handoff.Request
	statuses       []handoff.Status
	failure        error
	overrideResult func(*handoff.Result)
}

func (runner *fakeRunner) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	runner.requests = append(runner.requests, request)
	if runner.failure != nil {
		return handoff.Result{}, runner.failure
	}
	status := handoff.StatusCompleted
	if len(runner.statuses) > 0 {
		status = runner.statuses[0]
		runner.statuses = runner.statuses[1:]
	}
	result := handoff.Result{
		SchemaVersion: handoff.CurrentSchemaVersion,
		HandoffID:     request.ID,
		Role:          request.To,
		Status:        status,
		Summary:       "Proposal completed for " + string(request.To) + ".",
	}
	if runner.overrideResult != nil {
		runner.overrideResult(&result)
	}
	return result, nil
}
