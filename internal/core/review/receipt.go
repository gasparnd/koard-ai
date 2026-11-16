package review

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const ReceiptFileName = "receipt.json"

// Receipt is immutable, verifiable proof that one exact candidate snapshot
// completed the independent-review path: granted consent, this lifecycle's
// approved verdict, and the correction budget it spent. It is issued
// automatically the instant a lifecycle settles approved, and only then.
//
// "Verifiable" means content-addressed, the same as every other artifact in
// this package: ID is the SHA-256 digest of the receipt's own canonical
// content. A party verifies it by recomputing that digest and cross-checking
// Target against the stored lifecycle and snapshot — no signing key or
// external trust anchor is required or supported.
type Receipt struct {
	SchemaVersion   int              `json:"schema_version"`
	ID              string           `json:"id"`
	Target          consent.Target   `json:"target"`
	Reviewer        ReviewerIdentity `json:"reviewer"`
	Attempt         int              `json:"attempt"`
	MaxCorrections  int              `json:"max_corrections"`
	CorrectionsUsed int              `json:"corrections_used"`
}

// receiptContent is the subset of Receipt its own ID is computed over. ID is
// deliberately excluded, the same split candidate.Proposal/Candidate and
// snapshot's manifest-before-identity already use: a value cannot bind its
// own identity into the bytes that identity is a digest of.
type receiptContent struct {
	SchemaVersion   int              `json:"schema_version"`
	Target          consent.Target   `json:"target"`
	Reviewer        ReviewerIdentity `json:"reviewer"`
	Attempt         int              `json:"attempt"`
	MaxCorrections  int              `json:"max_corrections"`
	CorrectionsUsed int              `json:"corrections_used"`
}

func contentOf(record Lifecycle) receiptContent {
	return receiptContent{
		SchemaVersion:   CurrentSchemaVersion,
		Target:          record.Target,
		Reviewer:        record.Reviewer,
		Attempt:         record.Attempt,
		MaxCorrections:  record.MaxCorrections,
		CorrectionsUsed: record.CorrectionsUsed,
	}
}

func digestOfContent(content receiptContent) (string, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("encode receipt content: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}

// issueReceipt publishes the one immutable receipt for an approved lifecycle.
// Callers invoke it only while settling State == StateApproved, before that
// state is itself persisted, so a receipt that fails to write never leaves an
// approved lifecycle without one.
func issueReceipt(reviewDir string, record Lifecycle) error {
	content := contentOf(record)
	digest, err := digestOfContent(content)
	if err != nil {
		return err
	}
	receipt := Receipt{
		SchemaVersion:   content.SchemaVersion,
		ID:              digest,
		Target:          content.Target,
		Reviewer:        content.Reviewer,
		Attempt:         content.Attempt,
		MaxCorrections:  content.MaxCorrections,
		CorrectionsUsed: content.CorrectionsUsed,
	}
	return writeOnce(filepath.Join(reviewDir, ReceiptFileName), receipt)
}

// ReceiptStatus reads and strictly verifies the receipt for one workflow. A
// missing receipt is not an error — most lifecycles never reach approved. One
// that exists must have a self-consistent digest, and must match the
// currently stored, validated lifecycle and the exact snapshot bytes it
// names; any mismatch fails closed instead of returning a receipt that can
// no longer be trusted.
func ReceiptStatus(runDir string) (*Receipt, error) {
	reviewDir := filepath.Join(runDir, DirectoryName)
	path := filepath.Join(reviewDir, ReceiptFileName)
	var stored Receipt
	if err := decodeFile(path, &stored); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateReceipt(stored); err != nil {
		return nil, err
	}

	expected, err := digestOfContent(receiptContent{
		SchemaVersion:   stored.SchemaVersion,
		Target:          stored.Target,
		Reviewer:        stored.Reviewer,
		Attempt:         stored.Attempt,
		MaxCorrections:  stored.MaxCorrections,
		CorrectionsUsed: stored.CorrectionsUsed,
	})
	if err != nil {
		return nil, err
	}
	if expected != stored.ID {
		return nil, fmt.Errorf("receipt id %s does not match its content (expected %s)", stored.ID, expected)
	}

	record, err := Status(runDir)
	if err != nil {
		return nil, err
	}
	if record == nil || record.State != StateApproved ||
		record.Target != stored.Target || record.Reviewer != stored.Reviewer ||
		record.Attempt != stored.Attempt || record.CorrectionsUsed != stored.CorrectionsUsed {
		return nil, errors.New("receipt does not match the current approved review lifecycle")
	}

	// The receipt for attempt 1 was issued for the candidate workflow.json
	// declared at claim time. workflow.json is not write-once like every
	// other artifact here (settle replaces it with a plain atomic rename),
	// so re-resolving it now and requiring an exact match is what actually
	// catches a candidate going stale: consent.Status already fails closed
	// whenever the live workflow record no longer matches what was granted
	// and reviewed. Attempt 2 has no such external record to drift from —
	// its revised candidate is entirely owned, immutably, by this package.
	if stored.Attempt == 1 {
		current, _, err := consent.Status(runDir)
		if err != nil {
			return nil, fmt.Errorf("the reviewed candidate is stale: %w", err)
		}
		if current != stored.Target {
			return nil, errors.New("the reviewed candidate is stale: the workflow's current candidate no longer matches the receipted one")
		}
	}

	// A receipt for attempt 2 was issued for the revised candidate a granted
	// correction produced, which lives under the correction snapshot
	// directory, never the original snapshot the first attempt reviewed.
	snapshotDir := filepath.Join(runDir, "snapshot")
	if stored.Attempt > 1 {
		snapshotDir = filepath.Join(reviewDir, CorrectionSnapshotDirectory)
	}
	if _, err := snapshot.Load(snapshotDir, stored.Target.SnapshotID); err != nil {
		return nil, fmt.Errorf("verify receipt snapshot: %w", err)
	}
	return &stored, nil
}

func validateReceipt(receipt Receipt) error {
	if receipt.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("receipt schema_version %d is not supported", receipt.SchemaVersion)
	}
	if !digestPattern.MatchString(receipt.ID) {
		return errors.New("receipt id is malformed")
	}
	if err := validateTarget(receipt.Target); err != nil {
		return err
	}
	if err := validateReviewer(receipt.Reviewer); err != nil {
		return err
	}
	if receipt.MaxCorrections != MaxCorrections || receipt.CorrectionsUsed < 0 || receipt.CorrectionsUsed > MaxCorrections {
		return errors.New("receipt correction budget is invalid")
	}
	if receipt.Attempt < 1 || receipt.Attempt > MaxCorrections+1 {
		return errors.New("receipt attempt is outside its bounded range")
	}
	return nil
}
