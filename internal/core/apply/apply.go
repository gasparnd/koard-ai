// Package apply owns the first real workspace-write capability in Koard:
// taking a receipted, non-stale candidate and writing its exact bytes to
// the real workspace. It never invokes git and never authorizes commit,
// push, pull request, merge, or release — those remain entirely separate,
// human-driven decisions made outside this package.
package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const (
	CurrentSchemaVersion = 1
	FileName             = "apply.json"
)

type State string

const StateApplied State = "applied"

// Record is the completion marker for one workflow's apply. Unlike every
// write-once artifact review and receipt persist, it is deliberately
// mutable — the same reasoning workflow.json already uses — because it
// exists purely to make a repeat Apply call a safe, fast no-op, not to
// serve as evidence of anything.
type Record struct {
	SchemaVersion int            `json:"schema_version"`
	Target        consent.Target `json:"target"`
	State         State          `json:"state"`
}

// Apply writes every entry of the receipted candidate snapshot to workspace,
// at the relative path its repo:// URI names, overwriting whatever is
// already there. It requires nothing beyond a verified, non-stale,
// approved receipt — the same authority already granted through consent,
// review, and (if one happened) a separately decided correction.
//
// Content is deterministic from the snapshot and each file is written
// atomically, so interruption at any point — mid-file, between files, or
// after full completion — is handled by the same mechanism: calling Apply
// again re-verifies and re-writes every entry, converging on the identical
// correct end state. A repeat call whose own completion marker already
// names this exact target short-circuits without touching the filesystem
// again.
func Apply(runDir, workspace string) (Record, error) {
	if strings.TrimSpace(runDir) == "" {
		return Record{}, errors.New("run directory is required")
	}
	receipt, err := review.ReceiptStatus(runDir)
	if err != nil {
		return Record{}, fmt.Errorf("resolve review receipt: %w", err)
	}
	if receipt == nil {
		return Record{}, errors.New("apply requires an approved, verified receipt for this workflow")
	}

	if existing, err := Status(runDir); err != nil {
		return Record{}, err
	} else if existing != nil && existing.Target == receipt.Target {
		return *existing, nil
	}

	root, err := snapshot.RealWorkspace(workspace)
	if err != nil {
		return Record{}, err
	}
	snapshotDir := review.SnapshotDirectory(runDir, receipt.Attempt)
	stored, err := snapshot.Load(snapshotDir, receipt.Target.SnapshotID)
	if err != nil {
		return Record{}, fmt.Errorf("verify candidate snapshot: %w", err)
	}

	// Every entry is read and its path validated before anything is
	// written, the same guarantee snapshot.Capture already gives its own
	// writes: a refused candidate leaves no partial change in the real
	// workspace, never a write started on entry N-1 before entry N failed.
	type write struct {
		target  string
		content []byte
	}
	writes := make([]write, 0, len(stored.Manifest.Entries))
	for _, entry := range stored.Manifest.Entries {
		content, err := snapshot.ReadEntry(snapshotDir, entry)
		if err != nil {
			return Record{}, fmt.Errorf("read candidate entry %q: %w", entry.URI, err)
		}
		relative, err := snapshot.CanonicalRelativePath(entry.URI)
		if err != nil {
			return Record{}, err
		}
		writes = append(writes, write{target: filepath.Join(root, relative), content: content})
	}
	for _, pending := range writes {
		if err := writeFileAtomic(pending.target, pending.content, 0o644); err != nil {
			return Record{}, fmt.Errorf("apply %q: %w", pending.target, err)
		}
	}

	record := Record{SchemaVersion: CurrentSchemaVersion, Target: receipt.Target, State: StateApplied}
	if err := writeRecord(runDir, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Status reads the completion marker for one workflow, without writing
// anything. A missing marker is not an error — most workflows are never
// applied — and returns nil, nil.
func Status(runDir string) (*Record, error) {
	path := filepath.Join(runDir, FileName)
	encoded, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read apply record: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("decode apply record: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode apply record: trailing data after the record")
	}
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	return &record, nil
}

func validateRecord(record Record) error {
	if record.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("apply record schema_version %d is not supported", record.SchemaVersion)
	}
	if record.State != StateApplied {
		return fmt.Errorf("apply record state %q is invalid", record.State)
	}
	if strings.TrimSpace(record.Target.WorkflowID) == "" ||
		strings.TrimSpace(record.Target.CandidateID) == "" ||
		strings.TrimSpace(record.Target.SnapshotID) == "" {
		return errors.New("apply record target is incomplete")
	}
	return nil
}

func writeRecord(runDir string, record Record) error {
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode apply record: %w", err)
	}
	encoded = append(encoded, '\n')
	return writeFileAtomic(filepath.Join(runDir, FileName), encoded, 0o644)
}

// writeFileAtomic writes content to a temporary file in path's own
// directory, then renames it into place, so a reader or a crash never
// observes a partially written file — the same technique every other
// package in this codebase already uses for its own state files.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return fmt.Errorf("set file mode: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
