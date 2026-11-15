package agentrouter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/koard-ai/koard/internal/core/eventstream"
	"github.com/koard-ai/koard/internal/core/handoff"
)

const CurrentSchemaVersion = 1

// ResultReferenceKind identifies a context reference that points at the
// persisted result of the previous handoff.
const ResultReferenceKind = "koard.handoff.result"

// Step is one hop of the fixed route: the originating role and the role that
// must respond.
type Step struct {
	From handoff.Role
	To   handoff.Role
}

// FixedRoute is the version 1 planner to executor to verifier route.
var FixedRoute = [2]Step{
	{From: handoff.RolePlanner, To: handoff.RoleExecutor},
	{From: handoff.RoleExecutor, To: handoff.RoleVerifier},
}

// Runner executes one validated handoff request and returns its result.
type Runner interface {
	Execute(ctx context.Context, request handoff.Request) (handoff.Result, error)
}

type Options struct {
	WorkflowID string
	TaskID     string
	Objective  string
	StateDir   string
	Runner     Runner
	Clock      func() time.Time
}

// Outcome is the persisted record of one routed workflow.
type Outcome struct {
	SchemaVersion int               `json:"schema_version"`
	WorkflowID    string            `json:"workflow_id"`
	TaskID        string            `json:"task_id"`
	Status        handoff.Status    `json:"status"`
	Handoffs      []handoff.Request `json:"handoffs"`
	Results       []handoff.Result  `json:"results"`
}

// Route executes the fixed route and persists every handoff and event. It
// never mutates the workspace and never authorizes a model-returned next
// transition.
func Route(ctx context.Context, options Options) (Outcome, error) {
	options, err := validateOptions(options)
	if err != nil {
		return Outcome{}, err
	}

	workflowDir := filepath.Join(options.StateDir, options.WorkflowID)
	// Mkdir is atomic: only one creator can claim a workflow identity, so a
	// concurrent or replayed run fails instead of interleaving state files.
	if err := os.Mkdir(workflowDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Outcome{}, fmt.Errorf("workflow %q already exists at %s", options.WorkflowID, workflowDir)
		}
		return Outcome{}, fmt.Errorf("create workflow %q: %w", options.WorkflowID, err)
	}

	handoffDir := filepath.Join(workflowDir, "handoffs")
	if err := os.MkdirAll(handoffDir, 0o755); err != nil {
		return Outcome{}, fmt.Errorf("create workflow state: %w", err)
	}

	events, err := os.OpenFile(filepath.Join(workflowDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Outcome{}, fmt.Errorf("open workflow events: %w", err)
	}
	defer events.Close()
	writer := eventstream.NewWriter(events)

	outcome := Outcome{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    options.WorkflowID,
		TaskID:        options.TaskID,
		Handoffs:      make([]handoff.Request, 0, len(FixedRoute)),
		Results:       make([]handoff.Result, 0, len(FixedRoute)),
	}
	sequence := uint64(0)
	objective := options.Objective
	var carried []handoff.Reference

	for index, step := range FixedRoute {
		request, err := newRequest(options, step, index+1, objective, carried)
		if err != nil {
			return Outcome{}, err
		}

		sequence++
		if err := writeEvent(writer, record(options, sequence, "handoff.started", "Handoff started.", request.ID)); err != nil {
			return Outcome{}, err
		}
		if _, err := writeJSON(filepath.Join(handoffDir, fmt.Sprintf("%d-request.json", index+1)), request); err != nil {
			return Outcome{}, err
		}

		result, err := options.Runner.Execute(ctx, request)
		if err != nil {
			return Outcome{}, fmt.Errorf("execute handoff %q: %w", request.ID, err)
		}
		if err := handoff.ValidateResultFor(request, result); err != nil {
			_ = writeDiagnostic(writer, record(options, sequence+1, "handoff.invalid", "Handoff result failed validation.", ""), err)
			return Outcome{}, fmt.Errorf("validate handoff %q result: %w", request.ID, err)
		}

		sequence++
		if err := writeEvent(writer, record(options, sequence, "handoff.completed", "Handoff completed.", request.ID)); err != nil {
			return Outcome{}, err
		}
		encoded, err := writeJSON(filepath.Join(handoffDir, fmt.Sprintf("%d-result.json", index+1)), result)
		if err != nil {
			return Outcome{}, err
		}

		outcome.Handoffs = append(outcome.Handoffs, request)
		outcome.Results = append(outcome.Results, result)
		outcome.Status = result.Status

		if result.Status != handoff.StatusCompleted {
			return persistOutcome(workflowDir, outcome)
		}
		// The next handoff must verify the artifact this one produced, so the
		// persisted result travels as a digest-bound context reference.
		carried = resultReferences(index+1, encoded, result)
		objective = fmt.Sprintf("Verify the result of the previous handoff for: %s", options.Objective)
	}

	return persistOutcome(workflowDir, outcome)
}

// resultReferences binds the next handoff to the exact bytes this one wrote:
// the persisted result file plus any artifact the responder declared.
func resultReferences(step int, encoded []byte, result handoff.Result) []handoff.Reference {
	references := make([]handoff.Reference, 0, 1+len(result.Artifacts))
	references = append(references, handoff.Reference{
		Kind:   ResultReferenceKind,
		URI:    path.Join("handoffs", fmt.Sprintf("%d-result.json", step)),
		Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)),
	})
	return append(references, result.Artifacts...)
}

func validateOptions(options Options) (Options, error) {
	if strings.TrimSpace(options.WorkflowID) == "" {
		return Options{}, errors.New("workflow_id is required")
	}
	if err := validateWorkflowID(options.WorkflowID); err != nil {
		return Options{}, err
	}
	if strings.TrimSpace(options.TaskID) == "" {
		return Options{}, errors.New("task_id is required")
	}
	if strings.TrimSpace(options.Objective) == "" {
		return Options{}, errors.New("objective is required")
	}
	if strings.TrimSpace(options.StateDir) == "" {
		return Options{}, errors.New("state directory is required")
	}
	if options.Runner == nil {
		return Options{}, errors.New("runner is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	return options, nil
}

func validateWorkflowID(id string) error {
	if id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("workflow_id %q must be a single path segment", id)
	}
	return nil
}

func newRequest(options Options, step Step, index int, objective string, carried []handoff.Reference) (handoff.Request, error) {
	request := handoff.Request{
		SchemaVersion: handoff.CurrentSchemaVersion,
		ID:            fmt.Sprintf("%s-%d", options.WorkflowID, index),
		TaskID:        options.TaskID,
		From:          step.From,
		To:            step.To,
		Objective:     objective,
		Context:       carried,
		Capabilities:  []string{"repository:read"},
		ExpectedOutput: handoff.OutputContract{
			Format: "application/json",
			Schema: "koard://schemas/handoff-result/v1",
		},
	}
	if err := request.Validate(); err != nil {
		return handoff.Request{}, fmt.Errorf("build handoff %d: %w", index, err)
	}
	return request, nil
}

func record(options Options, sequence uint64, name, message, handoffID string) eventstream.Record {
	return eventstream.Record{
		SchemaVersion: eventstream.CurrentSchemaVersion,
		ID:            fmt.Sprintf("%s-record-%d", options.WorkflowID, sequence),
		WorkflowID:    options.WorkflowID,
		Sequence:      sequence,
		OccurredAt:    options.Clock().UTC(),
		Kind:          eventstream.KindEvent,
		Event: &eventstream.Event{
			Name:      name,
			Message:   message,
			HandoffID: handoffID,
		},
	}
}

func writeEvent(writer *eventstream.Writer, record eventstream.Record) error {
	if err := writer.Write(record); err != nil {
		return fmt.Errorf("write workflow event: %w", err)
	}
	return nil
}

func writeDiagnostic(writer *eventstream.Writer, record eventstream.Record, cause error) error {
	record.Kind = eventstream.KindDiagnostic
	record.Event = nil
	record.Diagnostic = &eventstream.Diagnostic{
		Code:     "KOARD_HANDOFF_INVALID",
		Severity: eventstream.SeverityError,
		Message:  "Handoff result failed validation.",
		Detail:   cause.Error(),
	}
	if err := writer.Write(record); err != nil {
		return fmt.Errorf("write workflow diagnostic: %w", err)
	}
	return nil
}

func persistOutcome(workflowDir string, outcome Outcome) (Outcome, error) {
	if _, err := writeJSON(filepath.Join(workflowDir, "workflow.json"), outcome); err != nil {
		return Outcome{}, err
	}
	return outcome, nil
}

func writeJSON(filePath string, value any) ([]byte, error) {
	encoded, err := marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", filepath.Base(filePath), err)
	}
	if err := writeFileAtomic(filePath, encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func marshal(value any) ([]byte, error) {
	switch typed := value.(type) {
	case handoff.Request:
		return handoff.MarshalRequest(typed)
	case handoff.Result:
		return handoff.MarshalResult(typed)
	default:
		encoded, err := json.MarshalIndent(typed, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(encoded, '\n'), nil
	}
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	written, err := temporary.Write(data)
	if err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if written != len(data) {
		temporary.Close()
		return io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary state file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
