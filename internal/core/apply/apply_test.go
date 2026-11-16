package apply

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/reviewmode"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

type reviewerFunc func(context.Context, review.Request, string) (review.Result, error)

func (function reviewerFunc) Review(ctx context.Context, request review.Request, snapshotDir string) (review.Result, error) {
	return function(ctx, request, snapshotDir)
}

func modeOn() (reviewmode.Mode, error) { return reviewmode.On, nil }

// approvedFixture builds a workflow with the given files already captured,
// consented, and approved, returning its run directory and workspace.
func approvedFixture(t *testing.T, files map[string]string) (runDir, workspace string) {
	t.Helper()
	root := t.TempDir()
	workspace = filepath.Join(root, "workspace")
	runDir = filepath.Join(root, "workflow-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	artifacts := make([]handoff.Reference, 0, len(names))
	for _, name := range names {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, handoff.Reference{Kind: "source", URI: "repo://" + name})
	}

	candidateID := "sha256:" + strings.Repeat("a", 64)
	captured, err := snapshot.Capture(context.Background(), workspace, filepath.Join(runDir, "snapshot"), candidateID, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Assess(filepath.Join(runDir, "snapshot"), captured.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome := agentrouter.Outcome{
		SchemaVersion: agentrouter.CurrentSchemaVersion,
		WorkflowID:    filepath.Base(runDir),
		TaskID:        "task-1",
		Status:        handoff.StatusCompleted,
		Candidate: &candidate.Candidate{
			SchemaVersion: candidate.CurrentSchemaVersion,
			ID:            candidateID,
			TaskID:        "task-1",
			HandoffID:     "workflow-1-1",
			SnapshotID:    captured.ID,
		},
		Risk: &assessment,
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "workflow.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := consent.Decide(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}

	reviewer := reviewerFunc(func(_ context.Context, request review.Request, _ string) (review.Result, error) {
		return review.Result{SchemaVersion: review.CurrentSchemaVersion, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved}, nil
	})
	if _, err := review.RunInitial(context.Background(), review.Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: review.ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: modeOn}); err != nil {
		t.Fatal(err)
	}
	return runDir, workspace
}

func TestApplyRequiresAVerifiedReceipt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	runDir := filepath.Join(root, "workflow-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("expected a missing-receipt error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("refused apply wrote a record: %v", err)
	}
}

func TestApplyWritesTheExactReceiptedBytes(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"change.go": "package change\n"})

	// Overwrite the workspace with different, stale content after the
	// candidate was captured and reviewed: the assertion below can only
	// pass if Apply actually performs a write, not merely if the file
	// already happened to hold the right bytes.
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	record, err := Apply(runDir, workspace)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if record.State != StateApplied {
		t.Fatalf("unexpected record: %#v", record)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "change.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package change\n" {
		t.Fatalf("unexpected content: %q, want the receipted candidate to have overwritten the stale workspace file", content)
	}

	stored, err := Status(runDir)
	if err != nil || stored == nil || *stored != record {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestApplyPreservesTheExistingFileMode(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"script.sh": "#!/bin/sh\necho hi\n"})
	target := filepath.Join(workspace, "script.sh")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err != nil {
		t.Fatalf("apply: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("apply changed the existing file mode: got %v, want 0755 (the executable bit must survive)", info.Mode().Perm())
	}
}

func TestApplyRejectsEscapeThroughASymlinkedParentDirectory(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"sub/change.go": "package change\n"})

	// Simulate drift after capture: the directory the candidate's path
	// names is no longer a real directory inside the workspace, but a
	// symlink pointing outside it.
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(workspace, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "sub")); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err == nil || !strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("expected a workspace-escape error, got %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("apply wrote through the symlink, outside the workspace: %v", entries)
	}
}

func TestApplyIsIdempotentAndSkipsAlreadyAppliedWork(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"change.go": "package change\n"})

	first, err := Apply(runDir, workspace)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// A second call must trust its own completion marker instead of
	// re-touching the filesystem: a workspace that does not even exist
	// must still succeed, which is only possible if nothing was
	// re-resolved or rewritten.
	second, err := Apply(runDir, filepath.Join(workspace, "does-not-exist"))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if second != first {
		t.Fatalf("second apply=%#v, want identical to first=%#v", second, first)
	}
}

func TestApplyRecoversAfterAnInterruptedRun(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})

	// Simulate a process that died mid-run: one file from the candidate is
	// missing and no completion marker was ever written.
	if err := os.Remove(filepath.Join(workspace, "b.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("test setup is wrong: a completion marker already exists")
	}

	record, err := Apply(runDir, workspace)
	if err != nil {
		t.Fatalf("recovering apply: %v", err)
	}
	if record.State != StateApplied {
		t.Fatalf("unexpected record: %#v", record)
	}
	for name, want := range map[string]string{"a.go": "package a\n", "b.go": "package b\n"} {
		content, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil {
			t.Fatalf("missing %s after recovery: %v", name, err)
		}
		if string(content) != want {
			t.Fatalf("%s = %q, want %q", name, content, want)
		}
	}
}

func TestApplyRejectsCorruptedSnapshotBeforeWritingAnything(t *testing.T) {
	runDir, workspace := approvedFixture(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"})

	// a.go is left stale (different from its receipted content) so that an
	// accidental write to it — even though a.go itself is not corrupted —
	// would be observable below. If the assertion only compared a.go to
	// its already-correct receipted bytes, a write that happened anyway
	// would be indistinguishable from one that never did.
	if err := os.WriteFile(filepath.Join(workspace, "a.go"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	receipt, err := review.ReceiptStatus(runDir)
	if err != nil || receipt == nil {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
	stored, err := snapshot.Load(filepath.Join(runDir, "snapshot"), receipt.Target.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var corrupted snapshot.Entry
	for _, entry := range stored.Manifest.Entries {
		if entry.URI == "repo://b.go" {
			corrupted = entry
		}
	}
	if corrupted.URI == "" {
		t.Fatal("test setup is wrong: b.go entry not found")
	}
	blob := filepath.Join(runDir, "snapshot", "blobs", "sha256", strings.TrimPrefix(corrupted.Digest, "sha256:"))
	if err := os.Chmod(blob, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("corrupt"), 0o444); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err == nil || !strings.Contains(err.Error(), "no longer matches its digest") {
		t.Fatalf("expected a corrupt-snapshot error, got %v", err)
	}
	after, err := os.ReadFile(filepath.Join(workspace, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "stale\n" {
		t.Fatalf("a refused apply wrote a.go even though only b.go was corrupted: %q", after)
	}
	if _, err := os.Stat(filepath.Join(runDir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("refused apply wrote a completion record: %v", err)
	}
}

func TestApplyNeverTouchesGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	runDir, workspace := approvedFixture(t, map[string]string{"change.go": "package change\n"})

	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = workspace
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init")
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "add", "-A")
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "baseline")

	before, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err != nil {
		t.Fatalf("apply: %v", err)
	}

	after, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("apply created a commit")
	}
	staged, err := exec.Command("git", "-C", workspace, "diff", "--cached", "--name-only").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(staged)) != "" {
		t.Fatalf("apply staged changes: %q", staged)
	}
}

// TestApplyRejectsAnUnsafeManifestEntryBeforeWritingAnything hand-crafts a
// snapshot directory directly, bypassing snapshot.Capture entirely — Capture
// would never allow an unsafe path to be stored in the first place. This
// proves Apply's own defense in depth independently protects the real
// workspace, not merely that Capture is careful; the two checks happen to
// share one function, but a future change to either must not rely on that.
func TestApplyRejectsAnUnsafeManifestEntryBeforeWritingAnything(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	runDir := filepath.Join(root, "workflow-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	snapshotDir := filepath.Join(runDir, "snapshot")
	maliciousContent := []byte("malicious\n")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(maliciousContent))
	blobPath := filepath.Join(snapshotDir, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:"))
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blobPath, maliciousContent, 0o444); err != nil {
		t.Fatal(err)
	}

	candidateID := "sha256:" + strings.Repeat("a", 64)
	manifest := snapshot.Manifest{
		SchemaVersion: snapshot.CurrentSchemaVersion,
		CandidateID:   candidateID,
		Entries:       []snapshot.Entry{{Kind: "source", URI: "repo://../escape.txt", Digest: digest, Size: int64(len(maliciousContent))}},
	}
	manifestEncoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "manifest.json"), manifestEncoded, 0o444); err != nil {
		t.Fatal(err)
	}
	snapshotID := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestEncoded))

	assessment, err := risk.Assess(snapshotDir, snapshotID, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome := agentrouter.Outcome{
		SchemaVersion: agentrouter.CurrentSchemaVersion,
		WorkflowID:    filepath.Base(runDir),
		TaskID:        "task-1",
		Status:        handoff.StatusCompleted,
		Candidate: &candidate.Candidate{
			SchemaVersion: candidate.CurrentSchemaVersion,
			ID:            candidateID,
			TaskID:        "task-1",
			HandoffID:     "workflow-1-1",
			SnapshotID:    snapshotID,
		},
		Risk: &assessment,
	}
	outcomeEncoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "workflow.json"), outcomeEncoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := consent.Decide(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}

	reviewer := reviewerFunc(func(_ context.Context, request review.Request, _ string) (review.Result, error) {
		return review.Result{SchemaVersion: review.CurrentSchemaVersion, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved}, nil
	})
	if _, err := review.RunInitial(context.Background(), review.Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: review.ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: modeOn}); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(runDir, workspace); err == nil || !strings.Contains(err.Error(), "canonical relative path") {
		t.Fatalf("expected a path-safety error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("apply escaped the workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("refused apply wrote a completion record: %v", err)
	}
}
