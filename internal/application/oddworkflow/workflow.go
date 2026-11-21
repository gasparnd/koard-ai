// Package oddworkflow projects a guided ODD workflow over existing router,
// review and apply evidence. It never grants authority or invokes providers.
package oddworkflow

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
	"reflect"
	"strings"

	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/apply"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/eventstream"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/odd"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/reviewmode"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const FileName = "odd.json"
const Scope = "guided-proposal-through-authorized-apply"

type Role struct {
	Role     handoff.Role `json:"role"`
	Provider string       `json:"provider"`
	Model    string       `json:"model"`
}

// Preparation records actual technical preflight, not business exploration or
// model judgments. Machine is the initial checkpoint, not current authority.
type Preparation struct {
	SchemaVersion int               `json:"schema_version"`
	Scope         string            `json:"scope"`
	Machine       odd.Machine       `json:"machine"`
	TaskID        string            `json:"task_id"`
	Objective     string            `json:"objective"`
	Policy        string            `json:"policy"`
	Configuration handoff.Reference `json:"configuration"`
	Roles         []Role            `json:"roles"`
}

// Prepare inspects the workspace configuration, resolves the technical route,
// and records its roles. Provider construction is still the CLI's preflight.
func Prepare(ctx context.Context, cwd, id, task, objective, policy string, cfg config.Config) (Preparation, error) {
	if err := agentrouter.ValidateWorkflowID(id); err != nil {
		return Preparation{}, err
	}
	if strings.TrimSpace(task) == "" || strings.TrimSpace(objective) == "" {
		return Preparation{}, errors.New("task and objective are required")
	}
	data, err := snapshot.ReadReference(ctx, cwd, handoff.Reference{URI: "repo://.koard/koard.yaml"})
	if err != nil {
		return Preparation{}, err
	}
	parsed, err := config.Parse(bytes.NewReader(data))
	if err != nil {
		return Preparation{}, err
	}
	if !reflect.DeepEqual(parsed, cfg) {
		return Preparation{}, errors.New("configuration changed during ODD preflight")
	}
	if cfg.Workflow.Default != "odd" {
		return Preparation{}, errors.New("ODD requires workflow.default: odd")
	}
	if policy == "" {
		policy = agentrouter.PolicyFixed
	}
	steps, err := agentrouter.PolicySteps(policy)
	if err != nil {
		return Preparation{}, err
	}
	p := Preparation{SchemaVersion: 1, Scope: Scope, TaskID: task, Objective: objective, Policy: policy, Configuration: handoff.Reference{Kind: "configuration", URI: "repo://.koard/koard.yaml", Digest: digest(data)}}
	p.Machine, err = odd.New(id)
	if err != nil {
		return Preparation{}, err
	}
	agents := map[handoff.Role]config.AgentConfig{handoff.RolePlanner: cfg.Agents.Planner, handoff.RoleExecutor: cfg.Agents.Executor, handoff.RoleVerifier: cfg.Agents.Verifier}
	for _, step := range steps {
		a := agents[step.To]
		if (a.Provider != "codex" && a.Provider != "opencode") || strings.TrimSpace(a.Model) == "" || a.Model == "unset" {
			return Preparation{}, fmt.Errorf("unresolved provider/model for %s", step.To)
		}
		p.Roles = append(p.Roles, Role{step.To, a.Provider, a.Model})
	}
	// Authorization here is only the caller's read-only proposal request;
	// exploration is configuration inspection; resolution selects technical
	// providers; classification selects the bounded route. Later gates persist.
	for _, event := range []odd.Event{odd.EventAuthorizationResolved, odd.EventExplorationCompleted, odd.EventResolutionCompleted, odd.EventClassificationCompleted} {
		p.Machine, err = p.Machine.Apply(event)
		if err != nil {
			return Preparation{}, err
		}
	}
	return p, nil
}

// Initialize is called within the router's exclusive claim. Atomic no-replace
// publication prevents a partial checkpoint from looking like successful setup.
func (p Preparation) Initialize(cwd, runDir string) error {
	if err := p.validate(filepath.Base(runDir)); err != nil {
		return err
	}
	if _, err := snapshot.ReadReference(context.Background(), cwd, p.Configuration); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.CreateTemp(runDir, ".odd-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Link(f.Name(), filepath.Join(runDir, FileName))
}

func (p Preparation) validate(id string) error {
	if p.SchemaVersion != 1 || p.Scope != Scope || p.Machine.WorkflowID != id || p.Machine.State != odd.StateImplement || p.Machine.Revision != 4 || p.Machine.ResumeState != "" {
		return errors.New("invalid ODD preparation binding")
	}
	if err := p.Machine.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(p.TaskID) == "" || strings.TrimSpace(p.Objective) == "" || p.Configuration.Kind != "configuration" || p.Configuration.URI != "repo://.koard/koard.yaml" || len(p.Configuration.Digest) != 71 || !strings.HasPrefix(p.Configuration.Digest, "sha256:") {
		return errors.New("invalid ODD preparation evidence")
	}
	steps, err := agentrouter.PolicySteps(p.Policy)
	if err != nil {
		return err
	}
	if len(steps) != len(p.Roles) {
		return errors.New("ODD role count mismatch")
	}
	for i, s := range steps {
		r := p.Roles[i]
		if r.Role != s.To || (r.Provider != "codex" && r.Provider != "opencode") || strings.TrimSpace(r.Model) == "" || r.Model == "unset" {
			return errors.New("invalid ODD role resolution")
		}
	}
	return nil
}

type Progress struct {
	Machine odd.Machine
	Reason  string
	Actions []string // Read-only guidance, not execution authority.
	Target  *consent.Target
}

// Status derives a fresh projection. Revisions describe this projection from
// preparation, not an append-only history; reads never manufacture resume events.
func Status(ctx context.Context, cwd, id string, mode func() (reviewmode.Mode, error)) (Progress, error) {
	if err := agentrouter.ValidateWorkflowID(id); err != nil {
		return Progress{}, err
	}
	base := "repo://.koard/state/runs/" + id + "/"
	read := func(name string, out any) error {
		data, err := snapshot.ReadReference(ctx, cwd, handoff.Reference{URI: base + name})
		if err != nil {
			return err
		}
		return decode(data, out)
	}
	var p Preparation
	if err := read(FileName, &p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Progress{}, errors.New("not an ODD run: preparation is missing")
		}
		return Progress{}, err
	}
	if err := p.validate(id); err != nil {
		return Progress{}, err
	}
	progress := Progress{Machine: p.Machine}
	settle := func(state odd.State, reason string, actions ...string) (Progress, error) {
		progress.Reason = reason
		progress.Actions = actions
		var events []odd.Event
		switch state {
		case odd.StateBlocked:
			events = []odd.Event{odd.EventBlocked}
		case odd.StateStopped:
			events = []odd.Event{odd.EventStopped}
		case odd.StateCompleted:
			events = []odd.Event{odd.EventImplementationCompleted, odd.EventClosureCompleted}
		}
		for _, e := range events {
			var err error
			progress.Machine, err = progress.Machine.Apply(e)
			if err != nil {
				return Progress{}, err
			}
		}
		return progress, nil
	}
	var outcome agentrouter.Outcome
	if err := read("workflow.json", &outcome); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settle(odd.StateBlocked, "proposal_incomplete")
		}
		return Progress{}, err
	}
	if outcome.SchemaVersion != agentrouter.CurrentSchemaVersion || outcome.WorkflowID != id || outcome.TaskID != p.TaskID || outcome.Policy != p.Policy {
		return Progress{}, errors.New("ODD workflow binding mismatch")
	}
	if err := validateHandoffs(ctx, cwd, base, p, outcome); err != nil {
		return Progress{}, err
	}
	if outcome.Status != handoff.StatusCompleted {
		data, err := snapshot.ReadReference(ctx, cwd, handoff.Reference{URI: base + "events.jsonl"})
		if err != nil {
			return Progress{}, err
		}
		d := eventstream.NewDecoder(bytes.NewReader(data))
		cancelled := false
		for {
			r, err := d.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return Progress{}, err
			}
			if r.WorkflowID != id {
				return Progress{}, errors.New("ODD event workflow mismatch")
			}
			if r.Diagnostic != nil && r.Diagnostic.Code == "KOARD_WORKFLOW_CANCELLED" {
				cancelled = true
			}
		}
		if cancelled {
			return settle(odd.StateStopped, "proposal_cancelled")
		}
		return settle(odd.StateBlocked, "proposal_"+string(outcome.Status))
	}
	runDir := filepath.Join(cwd, ".koard", "state", "runs", id)
	target, decision, err := consent.Status(runDir)
	if err != nil {
		return Progress{}, err
	}
	progress.Target = &target
	q := "'" + strings.ReplaceAll(id, "'", "'\"'\"'") + "'"
	command := func(action string) string { return "koard review " + action + " --workflow " + q }
	if decision != nil && decision.Decision == consent.Declined {
		return settle(odd.StateStopped, "consent_declined")
	}
	lifecycle, err := review.Status(runDir)
	if err != nil {
		return Progress{}, err
	}
	if lifecycle != nil {
		if decision == nil || decision.Decision != consent.Granted {
			return Progress{}, errors.New("ODD review has no granted consent")
		}
		if lifecycle.Target.WorkflowID != id || (lifecycle.Attempt == 1 && lifecycle.Target != target) {
			return Progress{}, errors.New("ODD review target mismatch")
		}
		if lifecycle.State == review.StateApproved {
			receipt, err := review.ReceiptStatus(runDir)
			if err != nil {
				return Progress{}, err
			}
			if receipt == nil {
				return Progress{}, errors.New("ODD approved review has no receipt")
			}
			progress.Target = &receipt.Target
			applied, err := apply.Status(runDir)
			if err != nil {
				return Progress{}, err
			}
			if applied != nil {
				if applied.Target != receipt.Target {
					return Progress{}, errors.New("ODD apply target does not match the verified receipt")
				}
				return settle(odd.StateCompleted, "applied")
			}
			return settle(odd.StateImplement, "apply_required", command("apply"))
		}
		switch lifecycle.State {
		case review.StateRejected:
			return settle(odd.StateStopped, "review_rejected")
		case review.StateFailed, review.StateEscalated, review.StateReviewing:
			return settle(odd.StateBlocked, "review_"+string(lifecycle.State))
		case review.StateCorrectionRequired:
			request, err := review.CorrectionStatus(runDir)
			if err != nil {
				return Progress{}, err
			}
			if request == nil || request.Target != lifecycle.Target || request.ID != lifecycle.CorrectionRequestID {
				return Progress{}, errors.New("ODD correction request mismatch")
			}
			d, err := review.CorrectionDecisionStatus(runDir)
			if err != nil {
				return Progress{}, err
			}
			if d != nil && (d.Target != request.Target || d.CorrectionRequestID != request.ID) {
				return Progress{}, errors.New("ODD correction decision mismatch")
			}
			if d != nil && d.Decision == consent.Declined {
				return settle(odd.StateStopped, "correction_declined", command("run"))
			}
			on, err := modeOn(mode)
			if err != nil {
				return Progress{}, err
			}
			if !on {
				return settle(odd.StateBlocked, "review_disabled")
			}
			if d == nil {
				return settle(odd.StateBlocked, "correction_consent_required", command("correction grant"), command("correction decline"))
			}
			return settle(odd.StateImplement, "correction_ready", command("run"))
		}
	}
	on, err := modeOn(mode)
	if err != nil {
		return Progress{}, err
	}
	if !on {
		return settle(odd.StateBlocked, "review_disabled")
	}
	if decision == nil {
		return settle(odd.StateBlocked, "consent_required", command("consent grant"), command("consent decline"))
	}
	return settle(odd.StateImplement, "review_ready", command("run"))
}

func modeOn(read func() (reviewmode.Mode, error)) (bool, error) {
	if read == nil {
		return false, errors.New("review mode unavailable")
	}
	m, err := read()
	if err != nil {
		return false, err
	}
	if m != reviewmode.On && m != reviewmode.Off {
		return false, errors.New("invalid review mode")
	}
	return m == reviewmode.On, nil
}

func validateHandoffs(ctx context.Context, cwd, base string, p Preparation, out agentrouter.Outcome) error {
	steps, _ := agentrouter.PolicySteps(p.Policy)
	if len(out.Handoffs) > len(steps) || len(out.Results) > len(out.Handoffs) {
		return errors.New("ODD handoff count mismatch")
	}
	if out.Status == handoff.StatusCompleted && (len(out.Results) != len(steps) || len(out.Handoffs) != len(steps)) {
		return errors.New("ODD completed proposal is incomplete")
	}
	for i, r := range out.Handoffs {
		if r.ID != fmt.Sprintf("%s-%d", p.Machine.WorkflowID, i+1) || r.TaskID != p.TaskID || r.From != steps[i].From || r.To != steps[i].To || !reflect.DeepEqual(r.Capabilities, []string{"repository:read"}) {
			return errors.New("ODD handoff binding mismatch")
		}
		b, err := handoff.MarshalRequest(r)
		if err != nil {
			return err
		}
		if _, err := snapshot.ReadReference(ctx, cwd, handoff.Reference{URI: base + fmt.Sprintf("handoffs/%d-request.json", i+1), Digest: digest(b)}); err != nil {
			return err
		}
		if i < len(out.Results) {
			result := out.Results[i]
			if err := handoff.ValidateResultFor(r, result); err != nil {
				return err
			}
			if out.Status == handoff.StatusCompleted && result.Status != handoff.StatusCompleted {
				return errors.New("ODD completed proposal has an incomplete result")
			}
			b, err := handoff.MarshalResult(result)
			if err != nil {
				return err
			}
			if _, err := snapshot.ReadReference(ctx, cwd, handoff.Reference{URI: base + fmt.Sprintf("handoffs/%d-result.json", i+1), Digest: digest(b)}); err != nil {
				return err
			}
		}
	}
	return nil
}

func digest(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
func decode(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing ODD JSON")
	}
	return nil
}
