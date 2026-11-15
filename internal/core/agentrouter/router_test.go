package agentrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/eventstream"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/mutation"
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
	// An unbound result must not be recorded as a completed workflow. The run
	// stopped, so its record is terminal and failed.
	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Status != handoff.StatusFailed {
		t.Fatalf("persisted status is %q, expected failed", persisted.Status)
	}
	if len(persisted.Results) != 0 {
		t.Fatalf("an unbound result must not enter the record: %#v", persisted.Results)
	}
}

func TestRoutePersistsFailedOutcomeWhenRunnerFails(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{failure: errors.New("provider unavailable")}
	options := testOptions(t, runner)

	outcome, err := Route(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("the runner failure must still reach the caller, got %v", err)
	}
	if outcome.Status != handoff.StatusFailed {
		t.Fatalf("returned status is %q, expected failed", outcome.Status)
	}

	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Status != handoff.StatusFailed {
		t.Fatalf("persisted status is %q, expected failed", persisted.Status)
	}
	if persisted.WorkflowID != options.WorkflowID || persisted.TaskID != options.TaskID {
		t.Fatalf("the persisted record lost its identity: %#v", persisted)
	}
}

func TestRouteKeepsCompletedStepsInFailedOutcome(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{failureAt: 2, failure: errors.New("verifier unavailable")}
	options := testOptions(t, runner)

	outcome, err := Route(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "verifier unavailable") {
		t.Fatalf("expected the second handoff to fail, got %v", err)
	}
	if outcome.Status != handoff.StatusFailed {
		t.Fatalf("status is %q, expected failed", outcome.Status)
	}
	if len(outcome.Handoffs) != 1 || len(outcome.Results) != 1 {
		t.Fatalf("the completed step must survive, got %d handoffs and %d results", len(outcome.Handoffs), len(outcome.Results))
	}
	if outcome.Candidate == nil {
		t.Fatal("the completed executor step must keep its candidate")
	}

	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Status != handoff.StatusFailed || len(persisted.Handoffs) != 1 {
		t.Fatalf("the persisted record lost the completed step: %#v", persisted)
	}
	if persisted.Candidate == nil {
		t.Fatal("the persisted record lost the candidate")
	}
}

func TestRouteRecordsCancellationAsItsOwnTerminalRecord(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	// The process runner reports a killed process, not a context error, so the
	// router has to recognise the cancellation itself.
	runner := &fakeRunner{failure: errors.New("signal: killed"), cancel: cancel}
	options := testOptions(t, runner)

	outcome, err := Route(ctx, options)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the returned error must report the cancellation, got %v", err)
	}
	if outcome.Status != handoff.StatusFailed {
		t.Fatalf("returned status is %q, expected failed", outcome.Status)
	}

	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Status != handoff.StatusFailed {
		t.Fatalf("persisted status is %q, expected failed", persisted.Status)
	}
	if len(persisted.Results) != 0 {
		t.Fatalf("a cancelled handoff must not enter the record: %#v", persisted.Results)
	}

	records := readRecords(t, filepath.Join(options.StateDir, options.WorkflowID, "events.jsonl"))
	last := records[len(records)-1]
	if last.Kind != eventstream.KindDiagnostic || last.Diagnostic == nil {
		t.Fatalf("the cancellation was not recorded as a diagnostic: %#v", last)
	}
	if last.Diagnostic.Code != "KOARD_WORKFLOW_CANCELLED" {
		t.Fatalf("diagnostic code is %q, expected KOARD_WORKFLOW_CANCELLED", last.Diagnostic.Code)
	}
	// Nothing malfunctioned in the workflow, so the stop is a warning.
	if last.Diagnostic.Severity != eventstream.SeverityWarning {
		t.Fatalf("diagnostic severity is %q, expected warning", last.Diagnostic.Severity)
	}
	if !strings.Contains(last.Diagnostic.Detail, "workflow-1-1") {
		t.Fatalf("diagnostic detail %q does not name the cancelled handoff", last.Diagnostic.Detail)
	}
	if !strings.Contains(last.Diagnostic.Detail, context.Canceled.Error()) {
		t.Fatalf("diagnostic detail %q does not name the cause", last.Diagnostic.Detail)
	}
	for _, record := range records {
		if record.Kind == eventstream.KindDiagnostic && record.Diagnostic.Code == "KOARD_WORKFLOW_FAILED" {
			t.Fatal("a cancellation must not be recorded as a workflow failure")
		}
	}
}

func TestRouteDoesNotClaimIdentityWhenAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeRunner{}
	options := testOptions(t, runner)

	outcome, err := Route(ctx, options)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cancellation error, got %v", err)
	}
	if len(outcome.Handoffs) != 0 {
		t.Fatalf("a cancelled run reported handoffs: %#v", outcome.Handoffs)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("a cancelled run reached the runner %d times", len(runner.requests))
	}
	// A run that never starts spends nothing, so its identity stays available.
	if _, statErr := os.Stat(filepath.Join(options.StateDir, options.WorkflowID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a cancelled run must claim no identity, stat error: %v", statErr)
	}
}

func TestRouteStopsBetweenHandoffsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{cancelAfterFirst: cancel}
	options := testOptions(t, runner)

	outcome, err := Route(ctx, options)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cancellation error, got %v", err)
	}
	if len(outcome.Handoffs) != 1 || len(outcome.Results) != 1 {
		t.Fatalf("only the completed handoff may stay, got %d handoffs and %d results", len(outcome.Handoffs), len(outcome.Results))
	}
	if len(runner.requests) != 1 {
		t.Fatalf("the second handoff must not start, runner saw %d requests", len(runner.requests))
	}
	handoffsDir := filepath.Join(options.StateDir, options.WorkflowID, "handoffs")
	if _, statErr := os.Stat(filepath.Join(handoffsDir, requestFile(2))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the second request must not be written, stat error: %v", statErr)
	}

	records := readRecords(t, filepath.Join(options.StateDir, options.WorkflowID, "events.jsonl"))
	last := records[len(records)-1]
	if last.Diagnostic == nil || last.Diagnostic.Code != "KOARD_WORKFLOW_CANCELLED" {
		t.Fatalf("the last record is not the cancellation diagnostic: %#v", last)
	}
	if !strings.Contains(last.Diagnostic.Detail, "workflow-1-2") {
		t.Fatalf("diagnostic detail %q does not name the handoff that did not run", last.Diagnostic.Detail)
	}
}

func TestRouteRecordsFailureDiagnosticBoundToTheFailingHandoff(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{failureAt: 2, failure: errors.New("verifier unavailable")}
	options := testOptions(t, runner)

	if _, err := Route(context.Background(), options); err == nil {
		t.Fatal("expected a failure")
	}

	records := readRecords(t, filepath.Join(options.StateDir, options.WorkflowID, "events.jsonl"))
	last := records[len(records)-1]
	if last.Kind != eventstream.KindDiagnostic || last.Diagnostic == nil {
		t.Fatalf("the failure was not recorded as a diagnostic: %#v", last)
	}
	if last.Diagnostic.Code != failedWorkflowCode {
		t.Fatalf("diagnostic code is %q, expected %q", last.Diagnostic.Code, failedWorkflowCode)
	}
	if last.Diagnostic.Severity != eventstream.SeverityError {
		t.Fatalf("diagnostic severity is %q, expected error", last.Diagnostic.Severity)
	}
	// The failing handoff is named in the detail because an event stream record
	// cannot carry both an event and a diagnostic payload.
	if !strings.Contains(last.Diagnostic.Detail, "workflow-1-2") {
		t.Fatalf("diagnostic detail %q does not name the failing handoff", last.Diagnostic.Detail)
	}
	if !strings.Contains(last.Diagnostic.Detail, "verifier unavailable") {
		t.Fatalf("diagnostic detail %q does not name the cause", last.Diagnostic.Detail)
	}
}

func TestRouteKeepsOneDiagnosticForAnUnboundResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{overrideResult: func(result *handoff.Result) { result.Role = handoff.RolePlanner }}
	options := testOptions(t, runner)

	if _, err := Route(context.Background(), options); err == nil {
		t.Fatal("expected a binding failure")
	}

	records := readRecords(t, filepath.Join(options.StateDir, options.WorkflowID, "events.jsonl"))
	diagnostics := 0
	for _, record := range records {
		if record.Kind != eventstream.KindDiagnostic {
			continue
		}
		diagnostics++
		if record.Diagnostic.Code != invalidResultCode {
			t.Fatalf("diagnostic code is %q, expected %q", record.Diagnostic.Code, invalidResultCode)
		}
	}
	if diagnostics != 1 {
		t.Fatalf("expected exactly one diagnostic, got %d", diagnostics)
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

func TestRouteCarriesPreviousResultToNextHandoff(t *testing.T) {
	t.Parallel()

	artifact := handoff.Reference{Kind: "proposal", URI: "koard://proposals/task-1", Digest: "sha256:proposal"}
	runner := &fakeRunner{artifacts: []handoff.Reference{artifact}}
	options := testOptions(t, runner)
	outcome, err := Route(context.Background(), options)
	if err != nil {
		t.Fatalf("route workflow: %v", err)
	}

	if len(runner.requests[0].Context) != 0 {
		t.Fatalf("the first handoff must not claim prior context, got %#v", runner.requests[0].Context)
	}
	context := runner.requests[1].Context
	if len(context) != 2 {
		t.Fatalf("expected the result reference plus the propagated artifact, got %#v", context)
	}
	if context[0].Kind != "koard.handoff.result" || context[0].URI != "handoffs/1-result.json" {
		t.Fatalf("unexpected result reference: %#v", context[0])
	}
	if context[1] != artifact {
		t.Fatalf("the executor artifact was not propagated: %#v", context[1])
	}

	persisted := readFile(t, filepath.Join(options.StateDir, options.WorkflowID, "handoffs", "1-result.json"))
	if context[0].Digest != "sha256:"+digestOf(persisted) {
		t.Fatalf("reference digest %q does not match the persisted result", context[0].Digest)
	}
	if encoded, err := handoff.MarshalResult(outcome.Results[0]); err != nil || !bytes.Equal(encoded, persisted) {
		t.Fatalf("persisted result is not the bound result bytes (err=%v)", err)
	}
}

func TestRouteContextDigestIsUniquePerHandoff(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	options := testOptions(t, runner)
	if _, err := Route(context.Background(), options); err != nil {
		t.Fatalf("route workflow: %v", err)
	}
	for index, request := range runner.requests {
		if len(request.Context) > 1 {
			t.Fatalf("handoff %d carries unexpected extra context: %#v", index+1, request.Context)
		}
	}
	if got := len(runner.requests[1].Context); got != 1 {
		t.Fatalf("expected exactly one result reference on the second handoff, got %d", got)
	}
	if digest := runner.requests[1].Context[0].Digest; digest == "" || digest == "sha256:"+digestOf(nil) {
		t.Fatalf("empty result reference digest: %q", digest)
	}
}

func TestRouteRejectsMutatingCapabilitiesBeforeTouchingState(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	options := testOptions(t, runner)
	options.Capabilities = []string{mutation.CapabilityRead, "git:push"}

	_, err := Route(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "git:push") {
		t.Fatalf("expected a blocked mutation capability, got %v", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("a blocked run invoked the runner %d times", len(runner.requests))
	}
	entries, readErr := os.ReadDir(options.StateDir)
	if readErr != nil {
		t.Fatalf("read state dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("a blocked run left %d entries in the state dir", len(entries))
	}
}

func TestRouteRequestsAreReadOnly(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	if _, err := Route(context.Background(), testOptions(t, runner)); err != nil {
		t.Fatalf("route workflow: %v", err)
	}
	for index, request := range runner.requests {
		if len(request.Capabilities) != 1 || request.Capabilities[0] != mutation.CapabilityRead {
			t.Fatalf("handoff %d is not read-only: %#v", index+1, request.Capabilities)
		}
	}
}

func TestRouteProducesDeterministicCandidate(t *testing.T) {
	t.Parallel()

	firstOptions := testOptions(t, &fakeRunner{})
	first, err := Route(context.Background(), firstOptions)
	if err != nil {
		t.Fatalf("route workflow: %v", err)
	}
	if first.Candidate == nil {
		t.Fatal("a completed executor handoff must produce a candidate")
	}
	if !strings.HasPrefix(first.Candidate.ID, "sha256:") {
		t.Fatalf("unexpected candidate identity %q", first.Candidate.ID)
	}
	if first.Candidate.HandoffID != first.Handoffs[0].ID || first.Candidate.TaskID != firstOptions.TaskID {
		t.Fatalf("candidate provenance is not the executor handoff: %#v", first.Candidate)
	}

	secondOptions := testOptions(t, &fakeRunner{})
	secondOptions.WorkflowID = "workflow-2"
	second, err := Route(context.Background(), secondOptions)
	if err != nil {
		t.Fatalf("route second workflow: %v", err)
	}
	if second.Candidate == nil || second.Candidate.ID != first.Candidate.ID {
		t.Fatalf("identical proposals produced different identities: %#v and %#v", first.Candidate, second.Candidate)
	}
	if second.Candidate.HandoffID == first.Candidate.HandoffID {
		t.Fatal("provenance must differ between workflows")
	}

	persisted := readOutcome(t, filepath.Join(secondOptions.StateDir, secondOptions.WorkflowID, "workflow.json"))
	if persisted.Candidate == nil || persisted.Candidate.ID != second.Candidate.ID {
		t.Fatalf("candidate identity was not persisted: %#v", persisted.Candidate)
	}
}

func TestRouteOmitsCandidateWhenExecutorDoesNotComplete(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{statuses: []handoff.Status{handoff.StatusBlocked}}
	options := testOptions(t, runner)
	outcome, err := Route(context.Background(), options)
	if err != nil {
		t.Fatalf("route blocked workflow: %v", err)
	}
	if outcome.Candidate != nil {
		t.Fatalf("a blocked executor must not produce a candidate: %#v", outcome.Candidate)
	}
	persisted := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
	if persisted.Candidate != nil {
		t.Fatalf("a blocked workflow persisted a candidate: %#v", persisted.Candidate)
	}
}

func TestDecodeRecordsRejectsCorruptStream(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writer := eventstream.NewWriter(&buffer)
	record := validRecordForTest()
	if err := writer.Write(record); err != nil {
		t.Fatalf("write record: %v", err)
	}
	buffer.WriteString("{not json}\n")

	if _, err := decodeRecords(&buffer); err == nil {
		t.Fatal("a corrupt event line must not be treated as end of stream")
	}
}

func TestDecodeRecordsAcceptsEmptyStream(t *testing.T) {
	t.Parallel()

	records, err := decodeRecords(strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty stream must decode cleanly: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("empty stream produced %d records", len(records))
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

func decodeRecords(reader io.Reader) ([]eventstream.Record, error) {
	decoder := eventstream.NewDecoder(reader)
	var records []eventstream.Record
	for {
		record, err := decoder.Next()
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
}

func readRecords(t *testing.T, path string) []eventstream.Record {
	t.Helper()

	records, err := decodeRecords(openFile(t, path))
	if err != nil {
		t.Fatalf("decode event stream: %v", err)
	}
	return records
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func digestOf(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func validRecordForTest() eventstream.Record {
	return eventstream.Record{
		SchemaVersion: eventstream.CurrentSchemaVersion,
		ID:            "workflow-1-record-1",
		WorkflowID:    "workflow-1",
		Sequence:      1,
		OccurredAt:    time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
		Kind:          eventstream.KindEvent,
		Event:         &eventstream.Event{Name: "handoff.started", Message: "Handoff started.", HandoffID: "workflow-1-1"},
	}
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
	requests []handoff.Request
	statuses []handoff.Status
	failure  error
	// failureAt makes the failure happen on that 1-based call only. Zero means
	// every call fails.
	failureAt      int
	artifacts      []handoff.Reference
	overrideResult func(*handoff.Result)
	// cancel is called when the runner starts, before it reports its failure.
	cancel context.CancelFunc
	// cancelAfterFirst is called once the first call has produced its result.
	cancelAfterFirst context.CancelFunc
}

func (runner *fakeRunner) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	runner.requests = append(runner.requests, request)
	if runner.cancel != nil {
		runner.cancel()
	}
	if runner.failure != nil && (runner.failureAt == 0 || runner.failureAt == len(runner.requests)) {
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
		Artifacts:     runner.artifacts,
	}
	if runner.overrideResult != nil {
		runner.overrideResult(&result)
	}
	if runner.cancelAfterFirst != nil && len(runner.requests) == 1 {
		runner.cancelAfterFirst()
	}
	return result, nil
}
