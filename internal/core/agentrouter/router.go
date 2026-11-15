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

	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/eventstream"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/mutation"
)

const CurrentSchemaVersion = 1

// ResultReferenceKind identifies a context reference that points at the
// persisted result of the previous handoff.
const ResultReferenceKind = "koard.handoff.result"

const (
	// invalidResultCode marks a diagnostic for a result that failed binding.
	invalidResultCode = "KOARD_HANDOFF_INVALID"
	// failedWorkflowCode marks a diagnostic for a run that stopped for any
	// other reason.
	failedWorkflowCode = "KOARD_WORKFLOW_FAILED"
	// cancelledWorkflowCode marks a run the caller stopped on purpose. It is a
	// warning: nothing in the workflow malfunctioned.
	cancelledWorkflowCode = "KOARD_WORKFLOW_CANCELLED"
)

// cancelled reports that the caller's context stopped the run while handoffID
// was the handoff in progress or the next one to start.
type cancelled struct {
	handoffID string
	cause     error
}

func (c *cancelled) Error() string {
	return fmt.Sprintf("handoff %q did not complete: %v", c.handoffID, c.cause)
}

func (c *cancelled) Unwrap() error { return c.cause }

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
	// Capabilities requested for the routed handoffs. Empty means the
	// read-only set. Any value outside that set stops the run.
	Capabilities []string
}

// Outcome is the persisted record of one routed workflow.
type Outcome struct {
	SchemaVersion int                  `json:"schema_version"`
	WorkflowID    string               `json:"workflow_id"`
	TaskID        string               `json:"task_id"`
	Status        handoff.Status       `json:"status"`
	Candidate     *candidate.Candidate `json:"candidate,omitempty"`
	Handoffs      []handoff.Request    `json:"handoffs"`
	Results       []handoff.Result     `json:"results"`
}

// routing carries the state of one claimed workflow. Every step writes through
// it, so a run has exactly one persist point and no return path can abandon the
// record it already started.
type routing struct {
	options  Options
	dir      string
	events   *os.File
	writer   *eventstream.Writer
	sequence uint64
	outcome  Outcome
	// handoffID names the handoff currently executing, so a failure says where
	// the run stopped.
	handoffID string
	// diagnosed records that a specific diagnostic already explained the
	// failure, so a generic one is not appended on top of it.
	diagnosed bool
}

// Route executes the fixed route and persists every handoff and event. It
// never mutates the workspace and never authorizes a model-returned next
// transition.
func Route(ctx context.Context, options Options) (Outcome, error) {
	options, err := validateOptions(options)
	if err != nil {
		return Outcome{}, err
	}
	// A run cancelled before it starts spends nothing, so its identity stays
	// available for a later attempt.
	if err := ctx.Err(); err != nil {
		return Outcome{}, fmt.Errorf("workflow %q cancelled before it started: %w", options.WorkflowID, err)
	}

	route, err := claim(options)
	if err != nil {
		return Outcome{}, err
	}
	defer route.events.Close()

	return route.settle(route.run(ctx))
}

// claim takes the workflow identity and opens its stream. Once the directory
// exists the identity is spent, so every later path leaves a record behind.
func claim(options Options) (*routing, error) {
	workflowDir := filepath.Join(options.StateDir, options.WorkflowID)
	// Mkdir is atomic: only one creator can claim a workflow identity, so a
	// concurrent or replayed run fails instead of interleaving state files.
	if err := os.Mkdir(workflowDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("workflow %q already exists at %s", options.WorkflowID, workflowDir)
		}
		return nil, fmt.Errorf("create workflow %q: %w", options.WorkflowID, err)
	}

	route := &routing{options: options, dir: workflowDir, outcome: newOutcome(options)}
	if err := route.open(); err != nil {
		// The identity is already claimed, so it must explain why nothing ran
		// instead of staying silent.
		route.outcome.Status = handoff.StatusFailed
		_, persistErr := persistOutcome(workflowDir, route.outcome)
		return nil, errors.Join(err, persistErr)
	}
	return route, nil
}

func (route *routing) open() error {
	if err := os.MkdirAll(filepath.Join(route.dir, "handoffs"), 0o755); err != nil {
		return fmt.Errorf("create workflow state: %w", err)
	}
	events, err := os.OpenFile(filepath.Join(route.dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open workflow events: %w", err)
	}
	route.events = events
	route.writer = eventstream.NewWriter(events)
	return nil
}

// run executes the fixed route. It returns the first failure and leaves the
// outcome describing how far the workflow got.
func (route *routing) run(ctx context.Context) error {
	objective := route.options.Objective
	var carried []handoff.Reference

	for index, step := range FixedRoute {
		request, err := newRequest(route.options, step, index+1, objective, carried)
		if err != nil {
			return err
		}
		route.handoffID = request.ID
		if err := ctx.Err(); err != nil {
			return &cancelled{handoffID: request.ID, cause: err}
		}

		if err := route.emit("handoff.started", "Handoff started.", request.ID); err != nil {
			return err
		}
		if _, err := writeJSON(filepath.Join(route.dir, "handoffs", requestFileName(index+1)), request); err != nil {
			return err
		}

		result, err := route.options.Runner.Execute(ctx, request)
		if err != nil {
			// A killed process reports its signal, not the cancellation, so the
			// context decides what stopped the run.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return &cancelled{handoffID: request.ID, cause: ctxErr}
			}
			return fmt.Errorf("execute handoff %q: %w", request.ID, err)
		}
		if err := handoff.ValidateResultFor(request, result); err != nil {
			invalid := fmt.Errorf("validate handoff %q result: %w", request.ID, err)
			route.diagnosed = true
			if diagnoseErr := route.diagnose(invalidResultCode, "Handoff result failed validation.", invalid.Error()); diagnoseErr != nil {
				return errors.Join(invalid, diagnoseErr)
			}
			return invalid
		}

		if err := route.emit("handoff.completed", "Handoff completed.", request.ID); err != nil {
			return err
		}
		encoded, err := writeJSON(filepath.Join(route.dir, "handoffs", resultFileName(index+1)), result)
		if err != nil {
			return err
		}

		route.outcome.Handoffs = append(route.outcome.Handoffs, request)
		route.outcome.Results = append(route.outcome.Results, result)
		route.outcome.Status = result.Status

		if result.Status != handoff.StatusCompleted {
			return nil
		}
		// The executor is the role that proposes, so its completed result is
		// the candidate this workflow hands to review.
		if step.To == handoff.RoleExecutor {
			produced, err := candidate.Produce(request, result)
			if err != nil {
				return fmt.Errorf("produce candidate for handoff %q: %w", request.ID, err)
			}
			route.outcome.Candidate = &produced
		}
		// The next handoff must verify the artifact this one produced, so the
		// persisted result travels as a digest-bound context reference.
		carried = resultReferences(index+1, encoded, result)
		objective = fmt.Sprintf("Verify the result of the previous handoff for: %s", route.options.Objective)
	}
	return nil
}

// settle writes the workflow record exactly once. A claimed workflow always
// leaves a readable record, so a run that stopped is distinguishable from one
// that never started.
func (route *routing) settle(runErr error) (Outcome, error) {
	if runErr != nil {
		// A stopped run is terminal: whatever the last completed handoff
		// reported, the workflow did not finish.
		route.outcome.Status = handoff.StatusFailed
		if !route.diagnosed {
			var stop *cancelled
			var diagnoseErr error
			if errors.As(runErr, &stop) {
				diagnoseErr = route.diagnoseWithSeverity(cancelledWorkflowCode, eventstream.SeverityWarning, "Workflow was cancelled before it completed.", runErr.Error())
			} else {
				diagnoseErr = route.diagnose(failedWorkflowCode, "Workflow failed before it completed.", runErr.Error())
			}
			if diagnoseErr != nil {
				runErr = errors.Join(runErr, diagnoseErr)
			}
		}
	}

	persisted, persistErr := persistOutcome(route.dir, route.outcome)
	if persistErr != nil {
		// The failure that stopped the run is not replaced by the failure that
		// stopped the record from being written.
		return Outcome{}, errors.Join(runErr, persistErr)
	}
	return persisted, runErr
}

func (route *routing) emit(name, message, handoffID string) error {
	if err := route.writer.Write(route.base(name, message, handoffID)); err != nil {
		return fmt.Errorf("write workflow event: %w", err)
	}
	return nil
}

// diagnose records why the run stopped. The failing handoff is named in the
// detail because a record carries either an event or a diagnostic, never both.
func (route *routing) diagnose(code, message, detail string) error {
	return route.diagnoseWithSeverity(code, eventstream.SeverityError, message, detail)
}

func (route *routing) diagnoseWithSeverity(code string, severity eventstream.Severity, message, detail string) error {
	entry := route.base("", "", "")
	entry.Kind = eventstream.KindDiagnostic
	entry.Event = nil
	entry.Diagnostic = &eventstream.Diagnostic{
		Code:     code,
		Severity: severity,
		Message:  message,
		Detail:   detail,
	}
	if err := route.writer.Write(entry); err != nil {
		return fmt.Errorf("write workflow diagnostic: %w", err)
	}
	return nil
}

func (route *routing) base(name, message, handoffID string) eventstream.Record {
	route.sequence++
	return eventstream.Record{
		SchemaVersion: eventstream.CurrentSchemaVersion,
		ID:            fmt.Sprintf("%s-record-%d", route.options.WorkflowID, route.sequence),
		WorkflowID:    route.options.WorkflowID,
		Sequence:      route.sequence,
		OccurredAt:    route.options.Clock().UTC(),
		Kind:          eventstream.KindEvent,
		Event:         &eventstream.Event{Name: name, Message: message, HandoffID: handoffID},
	}
}

// resultReferences binds the next handoff to the exact bytes this one wrote:
// the persisted result file plus any artifact the responder declared.
func resultReferences(step int, encoded []byte, result handoff.Result) []handoff.Reference {
	references := make([]handoff.Reference, 0, 1+len(result.Artifacts))
	references = append(references, handoff.Reference{
		Kind:   ResultReferenceKind,
		URI:    path.Join("handoffs", resultFileName(step)),
		Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)),
	})
	return append(references, result.Artifacts...)
}

func newOutcome(options Options) Outcome {
	return Outcome{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    options.WorkflowID,
		TaskID:        options.TaskID,
		Handoffs:      make([]handoff.Request, 0, len(FixedRoute)),
		Results:       make([]handoff.Result, 0, len(FixedRoute)),
	}
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
	if len(options.Capabilities) == 0 {
		options.Capabilities = mutation.ReadOnlyCapabilities()
	}
	// The hard stop runs before any state is created, so a refused run leaves
	// nothing behind for a later attempt to trip over.
	if err := mutation.RequireReadOnly(options.Capabilities); err != nil {
		return Options{}, err
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
		Capabilities:  append([]string(nil), options.Capabilities...),
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

func requestFileName(step int) string {
	return fmt.Sprintf("%d-request.json", step)
}

func resultFileName(step int) string {
	return fmt.Sprintf("%d-result.json", step)
}
