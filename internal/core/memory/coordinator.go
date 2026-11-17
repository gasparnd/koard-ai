// Package memory coordinates advisory, project-scoped knowledge retention.
// Verifier assessments are fallible; this package binds evidence and enforces
// retention decisions, rather than claiming to prove semantic truth.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/koard-ai/koard/internal/core/handoff"
)

type Project struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

type Observation struct {
	ID      int64  `json:"id"`
	Project string `json:"project"`
	Scope   string `json:"scope"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
	State   string `json:"state"`
}

// Store resolves canonical project ownership and returns full observations, not
// search snippets. Create must not replace existing knowledge. An error after
// invoking Create is treated as an unknown outcome, never retried here.
type Store interface {
	Resolve(context.Context, string) (Project, error)
	Search(context.Context, string, string) ([]Observation, error)
	Create(context.Context, string, string, handoff.MemoryProposal) (Observation, error)
}

type Input struct {
	Request       handoff.Request
	Result        handoff.Result
	ProposalIndex int
}

type Plan struct {
	Version       int                    `json:"version"`
	ID            string                 `json:"id"`
	OperationID   string                 `json:"operation_id"`
	Project       Project                `json:"project"`
	RequestDigest string                 `json:"request_digest"`
	ResultDigest  string                 `json:"result_digest"`
	ProposalIndex int                    `json:"proposal_index"`
	Proposal      handoff.MemoryProposal `json:"proposal"`
	Related       []Observation          `json:"related,omitempty"`
	Action        string                 `json:"action"`
	Reasons       []string               `json:"reasons,omitempty"`
}

type Decision struct {
	PlanID string `json:"plan_id"`
	Answer string `json:"answer"`
}

type Outcome struct {
	PlanID        string   `json:"plan_id"`
	OperationID   string   `json:"operation_id"`
	Status        string   `json:"status"`
	ObservationID int64    `json:"observation_id,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
}

type Coordinator struct {
	Workspace  string
	JournalDir string
	Store      Store
	// ReadEvidence must resolve only admitted repository/snapshot references.
	// It must not fetch arbitrary model-supplied URLs or follow escaping symlinks.
	ReadEvidence func(context.Context, handoff.Reference) ([]byte, error)
}

// Assess is read-only. It never treats a failed lookup as absence of knowledge.
func (c *Coordinator) Assess(ctx context.Context, in Input) (Plan, error) {
	if c.Store == nil || c.ReadEvidence == nil {
		return Plan{}, errors.New("memory store and evidence reader are required")
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if err := handoff.ValidateResultFor(in.Request, in.Result); err != nil {
		return Plan{}, err
	}
	if in.ProposalIndex < 0 || in.ProposalIndex >= len(in.Result.MemoryProposals) {
		return Plan{}, errors.New("memory proposal index is out of range")
	}
	workspace, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || !filepath.IsAbs(workspace) {
		return Plan{}, errors.New("memory requires an existing absolute workspace")
	}
	project, err := c.Store.Resolve(ctx, workspace)
	if err != nil {
		return Plan{}, err
	}
	if strings.TrimSpace(project.Name) == "" || project.Name != strings.TrimSpace(project.Name) || project.Directory != workspace {
		return Plan{}, errors.New("memory project identity or workspace is unresolved")
	}
	request, _ := handoff.MarshalRequest(in.Request) // Already validated above.
	result, _ := handoff.MarshalResult(in.Result)
	plan := Plan{Version: 1, Project: project, RequestDigest: digest(request), ResultDigest: digest(result), ProposalIndex: in.ProposalIndex, Proposal: in.Result.MemoryProposals[in.ProposalIndex], Action: "automatic"}
	// Search results may change while a response is lost. The operation belongs
	// to the input, not the search-dependent decision target, so that change can
	// never grant a second write attempt for the same proposal.
	binding, _ := json.Marshal(struct {
		Version         int
		Project         Project
		Request, Result string
		Index           int
	}{plan.Version, project, plan.RequestDigest, plan.ResultDigest, in.ProposalIndex})
	plan.OperationID = digest(binding)
	if plan.Proposal.Sensitivity != "nonsensitive" {
		plan.Action, plan.Reasons = "skipped", []string{"privacy_not_cleared"}
		return identify(plan), nil
	}
	for _, ref := range plan.Proposal.Evidence {
		body, err := c.ReadEvidence(ctx, ref)
		if err != nil {
			return Plan{}, fmt.Errorf("read memory evidence: %w", err)
		}
		if len(body) > 8<<20 || digest(body) != ref.Digest {
			return Plan{}, errors.New("memory evidence does not match the bound digest or size limit")
		}
	}
	// The bounded lookup is a conservative doubt signal, not a semantic proof
	// that no contradiction exists anywhere in the store.
	items, err := c.Store.Search(ctx, project.Name, plan.Proposal.Title)
	if err != nil {
		return Plan{}, err
	}
	if len(items) > 20 {
		return Plan{}, errors.New("memory search exceeded 20 observations")
	}
	plan.Related = append([]Observation(nil), items...)
	sort.Slice(plan.Related, func(i, j int) bool { return plan.Related[i].ID < plan.Related[j].ID })
	duplicate, differs := false, false
	for i, item := range plan.Related {
		if item.ID <= 0 || item.Project != project.Name || item.Scope != "project" {
			return Plan{}, errors.New("memory observation ownership mismatch")
		}
		if len(item.Content) > 50000 || len(item.Title) > 4096 || (i > 0 && item.ID == plan.Related[i-1].ID) {
			return Plan{}, errors.New("memory search returned invalid observations")
		}
		if sameKnowledge(item, plan.Proposal) && item.State == "active" {
			duplicate = true
		} else {
			differs = true
		}
	}
	if len(plan.Proposal.Uncertainty) > 0 {
		plan.Action = "pending"
		plan.Reasons = append(plan.Reasons, "verifier_uncertainty")
	}
	if differs {
		plan.Action = "pending"
		plan.Reasons = append(plan.Reasons, "related_knowledge_requires_decision")
	} else if duplicate && plan.Action != "pending" {
		plan.Action = "duplicate"
	}
	return identify(plan), nil
}

// Execute re-assesses current evidence, ownership and related knowledge before
// applying an exact decision. The durable attempt is claimed before Create.
func (c *Coordinator) Execute(ctx context.Context, in Input, decision *Decision) (Outcome, error) {
	plan, err := c.Assess(ctx, in)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{PlanID: plan.ID, OperationID: plan.OperationID, Status: plan.Action, Reasons: plan.Reasons}
	if decision != nil && (decision.PlanID != plan.ID || (decision.Answer != "granted" && decision.Answer != "declined")) {
		return out, errors.New("memory decision must name the exact current plan and granted or declined")
	}
	if plan.Action == "skipped" || plan.Action == "duplicate" {
		if decision != nil {
			return out, errors.New("memory decisions are not accepted for skipped or duplicate proposals")
		}
		return out, nil
	}
	if !filepath.IsAbs(c.JournalDir) {
		return out, errors.New("memory journal requires an absolute directory")
	}
	dir := filepath.Join(c.JournalDir, strings.TrimPrefix(plan.ID, "sha256:"))
	if err := ensureDirectory(dir); err != nil {
		return out, err
	}
	if err := persistExact(filepath.Join(dir, "plan.json"), plan); err != nil {
		return out, err
	}
	operationDir := filepath.Join(c.JournalDir, "operations", strings.TrimPrefix(plan.OperationID, "sha256:"))
	if err := ensureDirectory(operationDir); err != nil {
		return out, err
	}
	var saved Outcome
	found, err := readRecord(filepath.Join(operationDir, "outcome.json"), &saved)
	if err != nil {
		return out, err
	}
	if found {
		if saved.OperationID != plan.OperationID || saved.Status != "saved" || saved.ObservationID <= 0 || saved.PlanID == "" {
			return out, errors.New("invalid stored memory outcome")
		}
		if decision != nil && decision.Answer == "declined" {
			return out, errors.New("memory operation was already saved; decline cannot undo it")
		}
		return saved, nil
	}
	var attempt Decision
	found, err = readRecord(filepath.Join(operationDir, "attempt.json"), &attempt)
	if err != nil {
		return out, err
	}
	if found {
		out.Status = "unknown"
		return out, errors.New("memory attempt already exists without a confirmed outcome; do not retry")
	}
	var recorded Decision
	found, err = readRecord(filepath.Join(operationDir, "decision.json"), &recorded)
	if err != nil {
		return out, err
	}
	if found {
		if recorded.PlanID != plan.ID || (recorded.Answer != "automatic" && recorded.Answer != "granted" && recorded.Answer != "declined") || (recorded.Answer == "automatic" && plan.Action != "automatic") {
			return out, errors.New("invalid stored memory decision")
		}
		if decision != nil && *decision != recorded {
			return out, errors.New("memory decision conflicts with recorded decision")
		}
	} else {
		if decision == nil && plan.Action == "pending" {
			return out, nil
		}
		recorded = Decision{PlanID: plan.ID, Answer: "automatic"}
		if decision != nil {
			recorded = *decision
		}
		if err := persistExact(filepath.Join(operationDir, "decision.json"), recorded); err != nil {
			return out, err
		}
	}
	if recorded.Answer == "declined" {
		out.Status = "declined"
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	// Even a crash after claiming but before sending is unknown: conservatism
	// here prevents an ambiguous response from becoming a duplicate side effect.
	out.Status = "unknown"
	if err := publishRecord(filepath.Join(operationDir, "attempt.json"), Decision{PlanID: plan.ID, Answer: "attempted"}); err != nil {
		return out, fmt.Errorf("memory attempt already exists or could not be durably claimed; do not retry: %w", err)
	}
	item, err := c.Store.Create(ctx, plan.Project.Name, plan.OperationID, plan.Proposal)
	if err != nil {
		return out, fmt.Errorf("memory write outcome unknown; do not retry: %w", err)
	}
	if item.ID <= 0 || item.Project != plan.Project.Name || item.Scope != "project" || !sameKnowledge(item, plan.Proposal) {
		return out, errors.New("memory write acknowledgement mismatch; outcome unknown")
	}
	saved = Outcome{PlanID: plan.ID, OperationID: plan.OperationID, Status: "saved", ObservationID: item.ID}
	if err := persistExact(filepath.Join(operationDir, "outcome.json"), saved); err != nil {
		return out, err
	}
	return saved, nil
}

func sameKnowledge(item Observation, p handoff.MemoryProposal) bool {
	return item.Type == p.Type && item.Title == p.Title && item.Content == p.Content
}

func identify(plan Plan) Plan {
	plan.ID = ""
	encoded, _ := json.Marshal(plan)
	plan.ID = digest(encoded)
	return plan
}

func digest(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }
