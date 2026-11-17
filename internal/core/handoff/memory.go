package handoff

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// MemoryProposal is a model's suggestion, never verified knowledge or permission
// to persist. The application must assess evidence, sensitivity and uncertainty
// independently before deciding whether to retain it.
type MemoryProposal struct {
	Type        string      `json:"type"`
	Title       string      `json:"title"`
	Content     string      `json:"content"`
	Rationale   string      `json:"rationale"`
	Evidence    []Reference `json:"evidence"`
	Sensitivity string      `json:"sensitivity"`
	Uncertainty []string    `json:"uncertainty,omitempty"`
}

func (result Result) validateMemoryProposals() error {
	if len(result.MemoryProposals) == 0 {
		return nil
	}
	if result.Role != RoleVerifier || result.Status != StatusCompleted {
		return errors.New("memory_proposals require a completed verifier result")
	}
	if len(result.MemoryProposals) > 8 {
		return errors.New("memory_proposals permits at most 8 proposals")
	}
	for i, proposal := range result.MemoryProposals {
		if err := proposal.validate(); err != nil {
			return fmt.Errorf("memory_proposals[%d]: %w", i, err)
		}
	}
	return nil
}

func (proposal MemoryProposal) validate() error {
	switch proposal.Type {
	case "decision", "architecture", "bugfix", "pattern", "config", "discovery", "preference":
	default:
		return errors.New("type must be decision, architecture, bugfix, pattern, config, discovery or preference")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"title", proposal.Title, 256},
		{"content", proposal.Content, 4096},
		{"rationale", proposal.Rationale, 1024},
	} {
		if err := boundedMemoryText(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	switch proposal.Sensitivity {
	case "nonsensitive", "sensitive", "unknown":
	default:
		return errors.New("sensitivity must be nonsensitive, sensitive or unknown")
	}
	if len(proposal.Uncertainty) > 8 {
		return errors.New("uncertainty permits at most 8 reasons")
	}
	for _, reason := range proposal.Uncertainty {
		if err := boundedMemoryText("uncertainty", reason, 512); err != nil {
			return err
		}
	}
	if len(proposal.Evidence) == 0 || len(proposal.Evidence) > 8 {
		return errors.New("evidence requires 1 to 8 references")
	}
	for _, ref := range proposal.Evidence {
		if err := boundedMemoryText("evidence.kind", ref.Kind, 128); err != nil {
			return err
		}
		if err := boundedMemoryText("evidence.uri", ref.URI, 2048); err != nil {
			return err
		}
		if !strings.HasPrefix(ref.Digest, "sha256:") || len(ref.Digest) != 71 || strings.ToLower(ref.Digest) != ref.Digest {
			return errors.New("evidence.digest must be a lowercase sha256 digest")
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(ref.Digest, "sha256:")); err != nil {
			return errors.New("evidence.digest must be a lowercase sha256 digest")
		}
	}
	return nil
}

func boundedMemoryText(name, value string, limit int) error {
	if strings.TrimSpace(value) == "" || len(value) > limit {
		return fmt.Errorf("%s must be nonblank and at most %d bytes", name, limit)
	}
	return nil
}
