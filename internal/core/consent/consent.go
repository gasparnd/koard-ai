// Package consent records whether the user permits independent review of one
// exact candidate snapshot. Its authority never includes mutation or delivery.
package consent

import (
	"bytes"
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
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const (
	CurrentSchemaVersion = 1
	FileName             = "consent.json"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Decision is the user's candidate-scoped review decision.
type Decision string

const (
	Granted  Decision = "granted"
	Declined Decision = "declined"
)

// Authority names the only action a consent record permits.
type Authority string

const Review Authority = "review"

// Target is the exact persisted candidate snapshot a decision addresses.
type Target struct {
	WorkflowID  string    `json:"workflow_id"`
	CandidateID string    `json:"candidate_id"`
	SnapshotID  string    `json:"snapshot_id"`
	RiskTier    risk.Tier `json:"risk_tier"`
}

// Record is one immutable candidate-scoped decision.
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	Target        Target    `json:"target"`
	Decision      Decision  `json:"decision"`
	Authority     Authority `json:"authority"`
}

// Status resolves and verifies the current workflow target, then reads its
// consent record when one exists.
func Status(runDir string) (Target, *Record, error) {
	target, err := resolveTarget(runDir)
	if err != nil {
		return Target{}, nil, err
	}
	record, err := readRecord(filepath.Join(runDir, FileName))
	if err != nil {
		return Target{}, nil, err
	}
	if record != nil && record.Target != target {
		return Target{}, nil, errors.New("consent target does not match the current workflow target")
	}
	return target, record, nil
}

// Decide records one immutable review decision. Repeating the same decision
// for the same target is idempotent; changing it fails closed.
func Decide(runDir string, decision Decision) (Record, error) {
	if err := validateDecision(decision); err != nil {
		return Record{}, err
	}
	target, existing, err := Status(runDir)
	if err != nil {
		return Record{}, err
	}
	if existing != nil {
		return settleExisting(*existing, target, decision)
	}

	record := Record{
		SchemaVersion: CurrentSchemaVersion,
		Target:        target,
		Decision:      decision,
		Authority:     Review,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return Record{}, fmt.Errorf("encode consent record: %w", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(runDir, FileName)
	if err := writeOnce(path, encoded); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Record{}, fmt.Errorf("write consent record: %w", err)
		}
		concurrent, readErr := readRecord(path)
		if readErr != nil {
			return Record{}, readErr
		}
		if concurrent == nil {
			return Record{}, errors.New("consent record disappeared during publication")
		}
		return settleExisting(*concurrent, target, decision)
	}
	return record, nil
}

func resolveTarget(runDir string) (Target, error) {
	if strings.TrimSpace(runDir) == "" {
		return Target{}, errors.New("workflow run directory is required")
	}
	file, err := os.Open(filepath.Join(runDir, "workflow.json"))
	if err != nil {
		return Target{}, fmt.Errorf("read workflow record: %w", err)
	}
	defer file.Close()

	var outcome agentrouter.Outcome
	if err := decodeSingle(file, &outcome); err != nil {
		return Target{}, fmt.Errorf("decode workflow record: %w", err)
	}
	if outcome.SchemaVersion != agentrouter.CurrentSchemaVersion {
		return Target{}, fmt.Errorf("workflow schema_version %d is not supported", outcome.SchemaVersion)
	}
	if err := agentrouter.ValidateWorkflowID(outcome.WorkflowID); err != nil {
		return Target{}, err
	}
	if outcome.WorkflowID != filepath.Base(filepath.Clean(runDir)) {
		return Target{}, fmt.Errorf("workflow_id %q does not match run directory %q", outcome.WorkflowID, filepath.Base(filepath.Clean(runDir)))
	}
	if outcome.Status != handoff.StatusCompleted {
		return Target{}, fmt.Errorf("workflow %q is not completed", outcome.WorkflowID)
	}
	if outcome.Candidate == nil {
		return Target{}, fmt.Errorf("workflow %q has no candidate", outcome.WorkflowID)
	}
	if outcome.Risk == nil {
		return Target{}, fmt.Errorf("workflow %q has no risk assessment", outcome.WorkflowID)
	}
	if outcome.Candidate.SchemaVersion != candidate.CurrentSchemaVersion {
		return Target{}, fmt.Errorf("candidate schema_version %d is not supported", outcome.Candidate.SchemaVersion)
	}
	if !digestPattern.MatchString(outcome.Candidate.ID) {
		return Target{}, fmt.Errorf("candidate_id %q is malformed", outcome.Candidate.ID)
	}
	if !digestPattern.MatchString(outcome.Candidate.SnapshotID) {
		return Target{}, fmt.Errorf("snapshot_id %q is malformed", outcome.Candidate.SnapshotID)
	}
	if outcome.Candidate.TaskID != outcome.TaskID || strings.TrimSpace(outcome.Candidate.HandoffID) == "" {
		return Target{}, errors.New("candidate provenance does not match the workflow")
	}
	if outcome.Risk.SchemaVersion != risk.CurrentSchemaVersion {
		return Target{}, fmt.Errorf("risk schema_version %d is not supported", outcome.Risk.SchemaVersion)
	}
	if outcome.Risk.SnapshotID != outcome.Candidate.SnapshotID {
		return Target{}, fmt.Errorf("risk snapshot_id %q does not match candidate snapshot_id %q", outcome.Risk.SnapshotID, outcome.Candidate.SnapshotID)
	}
	if !validTier(outcome.Risk.Tier) {
		return Target{}, fmt.Errorf("risk tier %q is invalid", outcome.Risk.Tier)
	}
	loaded, err := snapshot.Load(filepath.Join(runDir, "snapshot"), outcome.Candidate.SnapshotID)
	if err != nil {
		return Target{}, fmt.Errorf("verify candidate snapshot: %w", err)
	}
	if loaded.Manifest.CandidateID != outcome.Candidate.ID {
		return Target{}, fmt.Errorf("snapshot candidate_id %q does not match candidate_id %q", loaded.Manifest.CandidateID, outcome.Candidate.ID)
	}
	return Target{
		WorkflowID:  outcome.WorkflowID,
		CandidateID: outcome.Candidate.ID,
		SnapshotID:  outcome.Candidate.SnapshotID,
		RiskTier:    outcome.Risk.Tier,
	}, nil
}

func readRecord(path string) (*Record, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read consent record: %w", err)
	}
	var record Record
	if err := decodeSingle(bytes.NewReader(encoded), &record); err != nil {
		return nil, fmt.Errorf("decode consent record: %w", err)
	}
	if err := record.validate(); err != nil {
		return nil, err
	}
	return &record, nil
}

func (record Record) validate() error {
	if record.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("consent schema_version %d is not supported", record.SchemaVersion)
	}
	if err := agentrouter.ValidateWorkflowID(record.Target.WorkflowID); err != nil {
		return err
	}
	if !digestPattern.MatchString(record.Target.CandidateID) {
		return fmt.Errorf("consent candidate_id %q is malformed", record.Target.CandidateID)
	}
	if !digestPattern.MatchString(record.Target.SnapshotID) {
		return fmt.Errorf("consent snapshot_id %q is malformed", record.Target.SnapshotID)
	}
	if !validTier(record.Target.RiskTier) {
		return fmt.Errorf("consent risk tier %q is invalid", record.Target.RiskTier)
	}
	if err := validateDecision(record.Decision); err != nil {
		return err
	}
	if record.Authority != Review {
		return fmt.Errorf("consent authority %q must be %q", record.Authority, Review)
	}
	return nil
}

func settleExisting(existing Record, target Target, decision Decision) (Record, error) {
	if existing.Target != target {
		return Record{}, errors.New("consent target does not match the current workflow target")
	}
	if existing.Decision != decision {
		return Record{}, fmt.Errorf("consent already records decision %q for this workflow", existing.Decision)
	}
	return existing, nil
}

func validateDecision(decision Decision) error {
	switch decision {
	case Granted, Declined:
		return nil
	default:
		return fmt.Errorf("consent decision %q must be %q or %q", decision, Granted, Declined)
	}
}

func validTier(tier risk.Tier) bool {
	return tier == risk.Passive || tier == risk.Medium || tier == risk.High
}

func decodeSingle(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("multiple JSON values are not supported")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func writeOnce(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".consent-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
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
	if err := os.Chmod(temporary.Name(), 0o444); err != nil {
		return err
	}
	return os.Link(temporary.Name(), path)
}
