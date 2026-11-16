// Package review owns the provider-neutral independent-review lifecycle.
// It verifies authority before invoking a reviewer, binds every result to the
// exact immutable snapshot, and never grants workspace mutation or delivery.
package review

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
	"regexp"
	"strings"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/reviewmode"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const (
	CurrentSchemaVersion        = 1
	DirectoryName               = "review"
	AttemptsDirectory           = "attempts"
	FileName                    = "review.json"
	CorrectionFileName          = "correction-request.json"
	CorrectionDecisionFileName  = "correction-decision.json"
	CorrectionSnapshotDirectory = "correction-snapshot"
	MaxCorrections              = 1
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type State string

const (
	StateReviewing          State = "reviewing"
	StateCorrectionRequired State = "correction_required"
	StateApproved           State = "approved"
	StateRejected           State = "rejected"
	StateFailed             State = "failed"
	StateEscalated          State = "escalated"
)

type Verdict string

const (
	VerdictApproved           Verdict = "approved"
	VerdictCorrectionRequired Verdict = "correction_required"
	VerdictRejected           Verdict = "rejected"
)

type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityBlocking Severity = "blocking"
)

// ReviewerIdentity is trusted application configuration, never model output.
type ReviewerIdentity struct {
	Runtime string `json:"runtime"`
	Model   string `json:"model"`
	Agent   string `json:"agent,omitempty"`
}

type Evidence struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
}

type Finding struct {
	ID       string     `json:"id"`
	Severity Severity   `json:"severity"`
	Summary  string     `json:"summary"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Request is the complete immutable input to one fresh reviewer invocation.
type Request struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	Attempt       int               `json:"attempt"`
	Target        consent.Target    `json:"target"`
	Reviewer      ReviewerIdentity  `json:"reviewer"`
	Capabilities  []string          `json:"capabilities"`
	Snapshot      snapshot.Manifest `json:"snapshot"`
}

// Result is model output. Reviewer identity is deliberately absent because the
// application, not the model, owns independence metadata.
type Result struct {
	SchemaVersion int            `json:"schema_version"`
	RequestID     string         `json:"request_id"`
	Attempt       int            `json:"attempt"`
	Target        consent.Target `json:"target"`
	Verdict       Verdict        `json:"verdict"`
	Findings      []Finding      `json:"findings,omitempty"`
}

// CorrectionRequest pauses the lifecycle. It carries no correction authority;
// a separate immutable decision is required by the next work unit.
type CorrectionRequest struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	Target        consent.Target `json:"target"`
	ReviewRequest string         `json:"review_request_id"`
	Findings      []Finding      `json:"findings"`
}

// Lifecycle is the strict current-state record. Immutable request/result files
// under attempts/ preserve the evidence behind each transition.
type Lifecycle struct {
	SchemaVersion       int              `json:"schema_version"`
	Target              consent.Target   `json:"target"`
	State               State            `json:"state"`
	Attempt             int              `json:"attempt"`
	Reviewer            ReviewerIdentity `json:"reviewer"`
	Verdict             Verdict          `json:"verdict,omitempty"`
	MaxCorrections      int              `json:"max_corrections"`
	CorrectionsUsed     int              `json:"corrections_used"`
	CorrectionRequestID string           `json:"correction_request_id,omitempty"`
	Failure             string           `json:"failure,omitempty"`
}

type Reviewer interface {
	Review(context.Context, Request, string) (Result, error)
}

type Options struct {
	RunDir           string
	Reviewer         Reviewer
	ReviewerIdentity ReviewerIdentity
	// ReviewMode resolves live review-mode authority. RunInitial calls it more
	// than once per attempt, so it must read current state on every call,
	// never return a value the caller captured earlier — otherwise a
	// revocation during claim preparation or a slow provider invocation would
	// go unnoticed.
	ReviewMode func() (reviewmode.Mode, error)
	// Workspace and Corrector are required only by RunCorrection. It runs
	// Corrector as a bounded, read-only handoff over Workspace and captures
	// whatever it proposes into a new immutable snapshot exactly like the
	// initial candidate, then independently re-reviews that revision.
	Workspace string
	Corrector agentrouter.Runner
}

// RunInitial claims and executes the first independent review attempt.
func RunInitial(ctx context.Context, options Options) (Lifecycle, error) {
	if err := validateOptions(options); err != nil {
		return Lifecycle{}, err
	}
	if err := ctx.Err(); err != nil {
		return Lifecycle{}, fmt.Errorf("review cancelled before it started: %w", err)
	}
	if err := requireReviewModeOn(options.ReviewMode); err != nil {
		return Lifecycle{}, err
	}
	target, decision, err := consent.Status(options.RunDir)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("resolve review consent: %w", err)
	}
	if decision == nil || decision.Decision != consent.Granted {
		return Lifecycle{}, errors.New("independent review requires granted consent for the exact candidate snapshot")
	}
	snapshotDir := filepath.Join(options.RunDir, "snapshot")
	stored, err := snapshot.Load(snapshotDir, target.SnapshotID)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("verify review snapshot: %w", err)
	}

	record := Lifecycle{SchemaVersion: CurrentSchemaVersion, Target: target, State: StateReviewing, Attempt: 1, Reviewer: options.ReviewerIdentity, MaxCorrections: MaxCorrections}
	reviewDir, err := claimReviewDir(options.RunDir, record)
	if err != nil {
		return Lifecycle{}, err
	}

	request := Request{SchemaVersion: CurrentSchemaVersion, ID: "review-1", Attempt: 1, Target: target, Reviewer: options.ReviewerIdentity, Capabilities: []string{"repository:read"}, Snapshot: stored.Manifest}
	if err := validateRequest(request); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	if err := writeOnce(filepath.Join(reviewDir, AttemptsDirectory, "1-request.json"), request); err != nil {
		return settleFailure(reviewDir, record, err)
	}

	// Re-checked immediately before handing control to a potentially
	// long-running provider: the first check only proves authority at claim
	// time, and a revocation afterward must still be caught before the
	// reviewer runs.
	if err := requireReviewModeOn(options.ReviewMode); err != nil {
		return settleFailure(reviewDir, record, err)
	}

	result, runErr := options.Reviewer.Review(ctx, request, snapshotDir)
	if runErr != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("review provider: %w", runErr))
	}
	if err := ctx.Err(); err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("review cancelled while provider was running: %w", err))
	}
	if err := validateResultFor(request, result); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	if err := writeOnce(filepath.Join(reviewDir, AttemptsDirectory, "1-result.json"), result); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	record.Verdict = result.Verdict
	switch result.Verdict {
	case VerdictApproved:
		record.State = StateApproved
	case VerdictRejected:
		record.State = StateRejected
	case VerdictCorrectionRequired:
		correction := newCorrectionRequest(request, result)
		if err := writeOnce(filepath.Join(reviewDir, CorrectionFileName), correction); err != nil {
			return settleFailure(reviewDir, record, err)
		}
		record.State = StateCorrectionRequired
		record.CorrectionRequestID = correction.ID
	}
	if err := writeCurrent(reviewDir, record); err != nil {
		return record, err
	}
	return record, nil
}

// CorrectionDecision is a separate immutable decision bound to the exact
// review target and the exact correction request it answers. It carries no
// authority beyond "RunCorrection may spend the one permitted correction
// attempt" — never workspace mutation or delivery.
type CorrectionDecision struct {
	SchemaVersion       int              `json:"schema_version"`
	Target              consent.Target   `json:"target"`
	CorrectionRequestID string           `json:"correction_request_id"`
	Decision            consent.Decision `json:"decision"`
}

// DecideCorrection records one immutable decision for the exact correction
// request a paused lifecycle is waiting on. Repeating the same decision is
// idempotent; a conflicting decision, or deciding outside a paused lifecycle,
// fails closed.
func DecideCorrection(runDir string, decision consent.Decision) (CorrectionDecision, error) {
	switch decision {
	case consent.Granted, consent.Declined:
	default:
		return CorrectionDecision{}, fmt.Errorf("correction decision %q must be %q or %q", decision, consent.Granted, consent.Declined)
	}
	record, err := Status(runDir)
	if err != nil {
		return CorrectionDecision{}, fmt.Errorf("resolve review lifecycle: %w", err)
	}
	if record == nil || record.State != StateCorrectionRequired {
		return CorrectionDecision{}, errors.New("correction decision requires a lifecycle paused for correction")
	}
	request, err := CorrectionStatus(runDir)
	if err != nil {
		return CorrectionDecision{}, fmt.Errorf("resolve correction request: %w", err)
	}
	if request == nil || request.ID != record.CorrectionRequestID {
		return CorrectionDecision{}, errors.New("correction request is missing or does not match the paused lifecycle")
	}

	proposed := CorrectionDecision{SchemaVersion: CurrentSchemaVersion, Target: record.Target, CorrectionRequestID: request.ID, Decision: decision}
	path := filepath.Join(runDir, DirectoryName, CorrectionDecisionFileName)
	if err := writeOnce(path, proposed); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return CorrectionDecision{}, err
		}
		existing, readErr := CorrectionDecisionStatus(runDir)
		if readErr != nil {
			return CorrectionDecision{}, readErr
		}
		if existing == nil {
			return CorrectionDecision{}, errors.New("correction decision disappeared during publication")
		}
		if existing.Target != proposed.Target || existing.CorrectionRequestID != proposed.CorrectionRequestID {
			return CorrectionDecision{}, errors.New("correction decision target does not match the current correction request")
		}
		if existing.Decision != decision {
			return CorrectionDecision{}, fmt.Errorf("correction decision already records %q for this correction request", existing.Decision)
		}
		return *existing, nil
	}
	return proposed, nil
}

func CorrectionDecisionStatus(runDir string) (*CorrectionDecision, error) {
	path := filepath.Join(runDir, DirectoryName, CorrectionDecisionFileName)
	var record CorrectionDecision
	if err := decodeFile(path, &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateCorrectionDecision(record); err != nil {
		return nil, err
	}
	return &record, nil
}

func validateCorrectionDecision(record CorrectionDecision) error {
	if record.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("correction decision schema_version %d is not supported", record.SchemaVersion)
	}
	if err := validateTarget(record.Target); err != nil {
		return err
	}
	if !digestPattern.MatchString(record.CorrectionRequestID) {
		return errors.New("correction decision correction_request_id is malformed")
	}
	switch record.Decision {
	case consent.Granted, consent.Declined:
		return nil
	default:
		return fmt.Errorf("correction decision %q must be %q or %q", record.Decision, consent.Granted, consent.Declined)
	}
}

// RunCorrection resolves the separate correction decision for the exact
// paused correction request. A declined decision settles the lifecycle as
// rejected without ever invoking a provider. A granted decision spends the
// one permitted correction: a fresh corrector proposes a revised candidate
// over Workspace, the proposal is captured into its own immutable snapshot
// exactly like the initial candidate, and a fresh reviewer independently
// re-reviews it. A correction_required verdict on this second attempt
// escalates instead of pausing again — MaxCorrections permits exactly one.
func RunCorrection(ctx context.Context, options Options) (Lifecycle, error) {
	if err := validateCorrectionOptions(options); err != nil {
		return Lifecycle{}, err
	}
	if err := ctx.Err(); err != nil {
		return Lifecycle{}, fmt.Errorf("correction cancelled before it started: %w", err)
	}

	reviewDir := filepath.Join(options.RunDir, DirectoryName)
	current, err := Status(options.RunDir)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("resolve review lifecycle: %w", err)
	}
	if current == nil || current.State != StateCorrectionRequired {
		return Lifecycle{}, errors.New("correction requires a lifecycle paused for correction")
	}
	record := *current
	correctionRequest, err := CorrectionStatus(options.RunDir)
	if err != nil {
		return record, fmt.Errorf("resolve correction request: %w", err)
	}
	if correctionRequest == nil || correctionRequest.ID != record.CorrectionRequestID {
		return record, errors.New("correction request is missing or does not match the paused lifecycle")
	}
	decision, err := CorrectionDecisionStatus(options.RunDir)
	if err != nil {
		return record, fmt.Errorf("resolve correction decision: %w", err)
	}
	if decision == nil || decision.CorrectionRequestID != correctionRequest.ID {
		return record, errors.New("correction requires a separate immutable decision for the exact correction request")
	}

	if decision.Decision == consent.Declined {
		record.State = StateRejected
		record.Verdict = VerdictRejected
		record.CorrectionRequestID = ""
		if err := writeCurrent(reviewDir, record); err != nil {
			return record, err
		}
		return record, nil
	}

	if record.CorrectionsUsed >= record.MaxCorrections {
		return record, errors.New("correction budget is exhausted")
	}
	if err := requireReviewModeOn(options.ReviewMode); err != nil {
		return record, err
	}

	snapshotDir := filepath.Join(options.RunDir, "snapshot")
	stored, err := snapshot.Load(snapshotDir, record.Target.SnapshotID)
	if err != nil {
		return record, fmt.Errorf("verify review snapshot: %w", err)
	}

	attempt := record.Attempt + 1
	correctionHandoff := handoff.Request{
		SchemaVersion:  handoff.CurrentSchemaVersion,
		ID:             fmt.Sprintf("correction-%d", attempt-1),
		TaskID:         fmt.Sprintf("%s-correction-%d", record.Target.WorkflowID, attempt-1),
		From:           handoff.RoleVerifier,
		To:             handoff.RoleExecutor,
		Objective:      correctionObjective(correctionRequest),
		Context:        snapshotReferences(stored.Manifest),
		Capabilities:   []string{"repository:read"},
		ExpectedOutput: handoff.OutputContract{Format: "application/json"},
	}
	if err := correctionHandoff.Validate(); err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("build correction handoff: %w", err))
	}

	// Re-checked immediately before the first provider call of this attempt.
	// Nothing has been persisted yet at this point (unlike RunInitial, this
	// lifecycle was already claimed by attempt 1), so a revocation here still
	// leaves the one permitted correction unspent and retryable once review
	// mode is back on, instead of burning it on a transient authority gap.
	if err := requireReviewModeOn(options.ReviewMode); err != nil {
		return record, err
	}
	proposal, runErr := options.Corrector.Execute(ctx, correctionHandoff)
	if runErr != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("correction provider: %w", runErr))
	}
	if err := ctx.Err(); err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("correction cancelled while provider was running: %w", err))
	}
	if err := handoff.ValidateResultFor(correctionHandoff, proposal); err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("correction result does not match its request: %w", err))
	}
	if proposal.Status != handoff.StatusCompleted {
		return settleFailure(reviewDir, record, fmt.Errorf("correction handoff ended with status %q", proposal.Status))
	}

	produced, err := candidate.Produce(correctionHandoff, proposal)
	if err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("produce revised candidate: %w", err))
	}
	correctionSnapshotDir := filepath.Join(reviewDir, CorrectionSnapshotDirectory)
	capturedSnapshot, err := snapshot.Capture(ctx, options.Workspace, correctionSnapshotDir, produced.ID, proposal.Artifacts)
	if err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("snapshot revised candidate: %w", err))
	}
	assessment, err := risk.Assess(correctionSnapshotDir, capturedSnapshot.ID, nil)
	if err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("assess revised candidate: %w", err))
	}
	revisedTarget := consent.Target{WorkflowID: record.Target.WorkflowID, CandidateID: produced.ID, SnapshotID: capturedSnapshot.ID, RiskTier: assessment.Tier}

	nextRequest := Request{SchemaVersion: CurrentSchemaVersion, ID: fmt.Sprintf("review-%d", attempt), Attempt: attempt, Target: revisedTarget, Reviewer: options.ReviewerIdentity, Capabilities: []string{"repository:read"}, Snapshot: capturedSnapshot.Manifest}
	if err := validateRequest(nextRequest); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	if err := writeOnce(filepath.Join(reviewDir, AttemptsDirectory, fmt.Sprintf("%d-request.json", attempt)), nextRequest); err != nil {
		return settleFailure(reviewDir, record, err)
	}

	if err := requireReviewModeOn(options.ReviewMode); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	result, runErr := options.Reviewer.Review(ctx, nextRequest, correctionSnapshotDir)
	if runErr != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("review provider: %w", runErr))
	}
	if err := ctx.Err(); err != nil {
		return settleFailure(reviewDir, record, fmt.Errorf("review cancelled while provider was running: %w", err))
	}
	if err := validateResultFor(nextRequest, result); err != nil {
		return settleFailure(reviewDir, record, err)
	}
	if err := writeOnce(filepath.Join(reviewDir, AttemptsDirectory, fmt.Sprintf("%d-result.json", attempt)), result); err != nil {
		return settleFailure(reviewDir, record, err)
	}

	record.Attempt = attempt
	record.CorrectionsUsed = record.CorrectionsUsed + 1
	record.Target = revisedTarget
	record.Verdict = result.Verdict
	record.CorrectionRequestID = ""
	switch result.Verdict {
	case VerdictApproved:
		record.State = StateApproved
	case VerdictRejected:
		record.State = StateRejected
	case VerdictCorrectionRequired:
		// The correction budget is now exhausted: a second pause would permit
		// an unbounded loop, so this terminates instead (S4).
		record.State = StateEscalated
	}
	if err := writeCurrent(reviewDir, record); err != nil {
		return record, err
	}
	return record, nil
}

func validateCorrectionOptions(options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	if options.Corrector == nil {
		return errors.New("corrector is required")
	}
	if strings.TrimSpace(options.Workspace) == "" {
		return errors.New("workspace is required")
	}
	return nil
}

func correctionObjective(request *CorrectionRequest) string {
	summaries := make([]string, 0, len(request.Findings))
	for _, finding := range request.Findings {
		summaries = append(summaries, finding.Summary)
	}
	return fmt.Sprintf("Address correction request %s: %s", request.ID, strings.Join(summaries, "; "))
}

func snapshotReferences(manifest snapshot.Manifest) []handoff.Reference {
	references := make([]handoff.Reference, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		references = append(references, handoff.Reference{Kind: entry.Kind, URI: entry.URI, Digest: entry.Digest})
	}
	return references
}

func Status(runDir string) (*Lifecycle, error) {
	path := filepath.Join(runDir, DirectoryName, FileName)
	var record Lifecycle
	if err := decodeFile(path, &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateLifecycle(record); err != nil {
		return nil, err
	}
	return &record, nil
}

func CorrectionStatus(runDir string) (*CorrectionRequest, error) {
	path := filepath.Join(runDir, DirectoryName, CorrectionFileName)
	var request CorrectionRequest
	if err := decodeFile(path, &request); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateCorrectionRequest(request); err != nil {
		return nil, err
	}
	return &request, nil
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.RunDir) == "" {
		return errors.New("run directory is required")
	}
	if options.Reviewer == nil {
		return errors.New("reviewer is required")
	}
	if options.ReviewMode == nil {
		return errors.New("review mode check is required")
	}
	return validateReviewer(options.ReviewerIdentity)
}

// requireReviewModeOn resolves live review-mode authority. RunInitial calls
// it both before the claim and again right before the reviewer runs (see
// Options.ReviewMode), so authority is held across the whole attempt instead
// of being proven once and assumed afterward.
func requireReviewModeOn(check func() (reviewmode.Mode, error)) error {
	mode, err := check()
	if err != nil {
		return fmt.Errorf("resolve review mode: %w", err)
	}
	if mode != reviewmode.On {
		return errors.New("independent review requires review mode to be on")
	}
	return nil
}

// claimReviewDir stages the attempts directory and the initial lifecycle
// record in a sibling temporary directory, then publishes them with a single
// atomic rename. A reader or a concurrent claimant therefore only ever
// observes no review directory or the fully formed one — never attempts/
// without review.json, or the reverse, which a crash between separate mkdir
// calls could otherwise leave behind undetected and unrecoverable.
func claimReviewDir(runDir string, record Lifecycle) (string, error) {
	reviewDir := filepath.Join(runDir, DirectoryName)
	if _, err := os.Stat(reviewDir); err == nil {
		return "", fmt.Errorf("review lifecycle already exists at %s", reviewDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect review lifecycle: %w", err)
	}

	staging, err := os.MkdirTemp(runDir, ".review-claim-*")
	if err != nil {
		return "", fmt.Errorf("stage review claim: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o755); err != nil {
		return "", fmt.Errorf("stage review claim: %w", err)
	}
	if err := os.Mkdir(filepath.Join(staging, AttemptsDirectory), 0o755); err != nil {
		return "", fmt.Errorf("stage review attempts: %w", err)
	}
	if err := writeCurrent(staging, record); err != nil {
		return "", fmt.Errorf("stage review lifecycle: %w", err)
	}

	// Two concurrent claimants race this rename, not the earlier Stat: the
	// loser fails here (the destination is non-empty, so the rename is
	// rejected rather than silently replacing it) and is reported the same
	// "already exists" way as the pre-check above.
	if err := os.Rename(staging, reviewDir); err != nil {
		if _, statErr := os.Stat(reviewDir); statErr == nil {
			return "", fmt.Errorf("review lifecycle already exists at %s", reviewDir)
		}
		return "", fmt.Errorf("claim review lifecycle: %w", err)
	}
	return reviewDir, nil
}

func validateReviewer(identity ReviewerIdentity) error {
	if strings.TrimSpace(identity.Runtime) == "" || strings.TrimSpace(identity.Model) == "" {
		return errors.New("reviewer runtime and model are required")
	}
	return nil
}

func validateRequest(request Request) error {
	if request.SchemaVersion != CurrentSchemaVersion || request.ID == "" || request.Attempt < 1 || request.Attempt > MaxCorrections+1 {
		return errors.New("review request metadata is invalid")
	}
	if err := validateTarget(request.Target); err != nil {
		return err
	}
	if err := validateReviewer(request.Reviewer); err != nil {
		return err
	}
	if len(request.Capabilities) != 1 || request.Capabilities[0] != "repository:read" {
		return errors.New("review request must contain only repository:read")
	}
	if request.Snapshot.SchemaVersion != snapshot.CurrentSchemaVersion || request.Snapshot.CandidateID != request.Target.CandidateID {
		return errors.New("review snapshot does not match the target candidate")
	}
	return nil
}

func validateResultFor(request Request, result Result) error {
	if result.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("review result schema_version %d is not supported", result.SchemaVersion)
	}
	if result.RequestID != request.ID {
		return fmt.Errorf("review result request_id %q does not match %q", result.RequestID, request.ID)
	}
	if result.Attempt != request.Attempt || result.Target != request.Target {
		return errors.New("review result does not match the request target and attempt")
	}
	switch result.Verdict {
	case VerdictApproved, VerdictRejected:
	case VerdictCorrectionRequired:
		if len(result.Findings) == 0 {
			return errors.New("correction_required review result needs findings")
		}
	default:
		return fmt.Errorf("review verdict %q is invalid", result.Verdict)
	}
	for _, finding := range result.Findings {
		if err := validateFinding(finding); err != nil {
			return err
		}
		if err := validateFindingEvidence(request.Snapshot, finding); err != nil {
			return err
		}
		if result.Verdict == VerdictApproved && finding.Severity == SeverityBlocking {
			return errors.New("approved review result cannot contain blocking findings")
		}
	}
	return nil
}

func validateFindingEvidence(manifest snapshot.Manifest, finding Finding) error {
	for _, evidence := range finding.Evidence {
		matched := false
		for _, entry := range manifest.Entries {
			if evidence.URI == entry.URI && evidence.Digest == entry.Digest {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("review finding %q evidence does not match the reviewed snapshot", finding.ID)
		}
	}
	return nil
}

func validateFinding(finding Finding) error {
	if strings.TrimSpace(finding.ID) == "" || strings.TrimSpace(finding.Summary) == "" {
		return errors.New("review finding id and summary are required")
	}
	if finding.Severity != SeverityWarning && finding.Severity != SeverityBlocking {
		return fmt.Errorf("review finding severity %q is invalid", finding.Severity)
	}
	for _, evidence := range finding.Evidence {
		if !strings.HasPrefix(evidence.URI, snapshot.RepoScheme) || !digestPattern.MatchString(evidence.Digest) {
			return errors.New("review finding evidence must bind a repo URI and sha256 digest")
		}
	}
	return nil
}

func validateTarget(target consent.Target) error {
	if strings.TrimSpace(target.WorkflowID) == "" || !digestPattern.MatchString(target.CandidateID) || !digestPattern.MatchString(target.SnapshotID) {
		return errors.New("review target identity is invalid")
	}
	return nil
}

func validateLifecycle(record Lifecycle) error {
	if record.SchemaVersion != CurrentSchemaVersion || record.MaxCorrections != MaxCorrections || record.CorrectionsUsed < 0 || record.CorrectionsUsed > MaxCorrections {
		return errors.New("review lifecycle metadata is invalid")
	}
	if err := validateTarget(record.Target); err != nil {
		return err
	}
	if err := validateReviewer(record.Reviewer); err != nil {
		return err
	}
	switch record.Target.RiskTier {
	case risk.Passive, risk.Medium, risk.High:
	default:
		return fmt.Errorf("review target risk tier %q is invalid", record.Target.RiskTier)
	}
	if record.Attempt < 1 || record.Attempt > record.MaxCorrections+1 {
		return errors.New("review lifecycle attempt is outside its bounded range")
	}
	switch record.State {
	case StateReviewing:
		if record.Verdict != "" || record.Failure != "" || record.CorrectionRequestID != "" {
			return errors.New("reviewing lifecycle contains a terminal outcome")
		}
	case StateCorrectionRequired:
		if record.Verdict != VerdictCorrectionRequired || !digestPattern.MatchString(record.CorrectionRequestID) || record.Failure != "" {
			return errors.New("correction_required lifecycle is inconsistent")
		}
	case StateApproved:
		if record.Verdict != VerdictApproved || record.Failure != "" || record.CorrectionRequestID != "" {
			return errors.New("approved lifecycle is inconsistent")
		}
	case StateRejected:
		if record.Verdict != VerdictRejected || record.Failure != "" || record.CorrectionRequestID != "" {
			return errors.New("rejected lifecycle is inconsistent")
		}
	case StateFailed:
		if strings.TrimSpace(record.Failure) == "" {
			return errors.New("failed lifecycle requires a failure")
		}
	case StateEscalated:
	default:
		return fmt.Errorf("review state %q is invalid", record.State)
	}
	if record.State != StateFailed && record.Failure != "" {
		return errors.New("non-failed lifecycle cannot contain a failure")
	}
	return nil
}

func validateCorrectionRequest(request CorrectionRequest) error {
	if request.SchemaVersion != CurrentSchemaVersion || !digestPattern.MatchString(request.ID) || request.ReviewRequest == "" || len(request.Findings) == 0 {
		return errors.New("correction request metadata is invalid")
	}
	if err := validateTarget(request.Target); err != nil {
		return err
	}
	for _, finding := range request.Findings {
		if err := validateFinding(finding); err != nil {
			return err
		}
	}
	return nil
}

func newCorrectionRequest(request Request, result Result) CorrectionRequest {
	seed, _ := json.Marshal(struct {
		Target   consent.Target `json:"target"`
		Request  string         `json:"request"`
		Findings []Finding      `json:"findings"`
	}{request.Target, request.ID, result.Findings})
	return CorrectionRequest{SchemaVersion: CurrentSchemaVersion, ID: fmt.Sprintf("sha256:%x", sha256.Sum256(seed)), Target: request.Target, ReviewRequest: request.ID, Findings: result.Findings}
}

func settleFailure(reviewDir string, record Lifecycle, failure error) (Lifecycle, error) {
	record.State = StateFailed
	record.Failure = failure.Error()
	if err := writeCurrent(reviewDir, record); err != nil {
		return record, errors.Join(failure, err)
	}
	return record, failure
}

func writeCurrent(reviewDir string, record Lifecycle) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(reviewDir, ".review-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(reviewDir, FileName)); err != nil {
		return err
	}
	return nil
}

func writeOnce(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return fmt.Errorf("write immutable review artifact %s: %w", path, err)
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		return fmt.Errorf("write immutable review artifact %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync immutable review artifact %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close immutable review artifact %s: %w", path, err)
	}
	return nil
}

func decodeFile(path string, value any) error {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: trailing data", path)
	}
	return nil
}
