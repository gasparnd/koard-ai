package candidate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/koard-ai/koard/internal/core/handoff"
)

const CurrentSchemaVersion = 1

// IDPrefix marks an identity as a SHA-256 digest of a canonical proposal.
const IDPrefix = "sha256:"

// Proposal is the content a candidate is identified by. It deliberately holds
// only what the handoff protocol actually carries, so the identity can be
// recomputed from persisted state alone.
type Proposal struct {
	SchemaVersion int                 `json:"schema_version"`
	Summary       string              `json:"summary"`
	Artifacts     []handoff.Reference `json:"artifacts,omitempty"`
	Evidence      []handoff.Reference `json:"evidence,omitempty"`
}

// Candidate is one proposal with a content-derived identity. Provenance is
// recorded next to the identity and is never part of the digest, so the same
// proposal produced by two runs still has one identity.
type Candidate struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	HandoffID     string `json:"handoff_id"`
	// SnapshotID is the identity of the captured bytes behind the proposal.
	// It is recorded beside the identity and never part of its digest.
	SnapshotID string `json:"snapshot_id,omitempty"`
}

// Produce derives a candidate from the exact result a responder returned. It
// rejects anything that is not a completed result bound to its request.
func Produce(request handoff.Request, result handoff.Result) (Candidate, error) {
	if err := handoff.ValidateResultFor(request, result); err != nil {
		return Candidate{}, fmt.Errorf("candidate requires a bound handoff result: %w", err)
	}
	if result.Status != handoff.StatusCompleted {
		return Candidate{}, fmt.Errorf("candidate requires a completed result, got %q", result.Status)
	}

	canonical, err := NewProposal(result).Canonical()
	if err != nil {
		return Candidate{}, err
	}

	return Candidate{
		SchemaVersion: CurrentSchemaVersion,
		ID:            fmt.Sprintf("%s%x", IDPrefix, sha256.Sum256(canonical)),
		TaskID:        request.TaskID,
		HandoffID:     request.ID,
	}, nil
}

// NewProposal projects the content-bearing fields of a result.
func NewProposal(result handoff.Result) Proposal {
	return Proposal{
		SchemaVersion: CurrentSchemaVersion,
		Summary:       result.Summary,
		Artifacts:     result.Artifacts,
		Evidence:      result.Evidence,
	}
}

// Canonical returns the stable encoding the identity is computed over. Field
// order is fixed by the struct, and artifact order is preserved because it is
// part of the proposal.
func (proposal Proposal) Canonical() ([]byte, error) {
	encoded, err := json.Marshal(proposal)
	if err != nil {
		return nil, fmt.Errorf("encode proposal: %w", err)
	}
	return encoded, nil
}
