package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/reviewmode"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

func TestReceiptStatusIsNilBeforeAnyReview(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	receipt, err := ReceiptStatus(runDir)
	if err != nil || receipt != nil {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
}

func TestRunInitialIssuesReceiptOnlyWhenApproved(t *testing.T) {
	for _, verdict := range []Verdict{VerdictApproved, VerdictRejected} {
		t.Run(string(verdict), func(t *testing.T) {
			runDir, _ := reviewFixture(t, consent.Granted)
			reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
				return boundResult(request, verdict), nil
			})
			record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := ReceiptStatus(runDir)
			if err != nil {
				t.Fatalf("receipt status: %v", err)
			}
			if verdict == VerdictApproved {
				if receipt == nil {
					t.Fatal("expected a receipt for an approved review")
				}
				if receipt.Target != record.Target || receipt.Reviewer != record.Reviewer || receipt.Attempt != record.Attempt || receipt.CorrectionsUsed != record.CorrectionsUsed {
					t.Fatalf("receipt=%#v record=%#v", receipt, record)
				}
				if _, err := os.Stat(filepath.Join(runDir, DirectoryName, ReceiptFileName)); err != nil {
					t.Fatalf("receipt file missing: %v", err)
				}
			} else if receipt != nil {
				t.Fatalf("unexpected receipt for a %s review: %#v", verdict, receipt)
			}
		})
	}
}

func TestReceiptStatusIsNilForEveryNonApprovedState(t *testing.T) {
	t.Run("correction_required", func(t *testing.T) {
		runDir, _ := pausedFixture(t)
		receipt, err := ReceiptStatus(runDir)
		if err != nil || receipt != nil {
			t.Fatalf("receipt=%#v err=%v", receipt, err)
		}
	})

	t.Run("failed", func(t *testing.T) {
		runDir, _ := reviewFixture(t, consent.Granted)
		reviewer := reviewerFunc(func(context.Context, Request, string) (Result, error) {
			return Result{}, errors.New("provider unavailable")
		})
		if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err == nil {
			t.Fatal("expected a provider failure")
		}
		receipt, err := ReceiptStatus(runDir)
		if err != nil || receipt != nil {
			t.Fatalf("receipt=%#v err=%v", receipt, err)
		}
	})

	t.Run("escalated", func(t *testing.T) {
		runDir, _ := pausedFixture(t)
		if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
			t.Fatal(err)
		}
		workspace := workspaceFor(runDir)
		corrector := correctingProposal(workspace)
		reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
			result := boundResult(request, VerdictCorrectionRequired)
			result.Findings = []Finding{{ID: "F2", Severity: SeverityBlocking, Summary: "Still broken", Evidence: []Evidence{{URI: "repo://change.go", Digest: request.Snapshot.Entries[0].Digest}}}}
			return result, nil
		})
		if _, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector}); err != nil {
			t.Fatal(err)
		}
		receipt, err := ReceiptStatus(runDir)
		if err != nil || receipt != nil {
			t.Fatalf("receipt=%#v err=%v", receipt, err)
		}
	})
}

func TestRunCorrectionIssuesReceiptForTheRevisedCandidate(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceFor(runDir)
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n\n// fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrector := correctingProposal(workspace)
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: approvingReviewer(t, 2), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := ReceiptStatus(runDir)
	if err != nil || receipt == nil {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
	if receipt.Attempt != 2 || receipt.CorrectionsUsed != 1 || receipt.Target != record.Target {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
}

func TestReceiptStatusDetectsTamperedContent(t *testing.T) {
	runDir := approvedReceiptFixture(t)
	receipt := readStoredReceipt(t, runDir)
	receipt.Attempt = 2 // tampered without updating the digest
	overwriteReceipt(t, runDir, receipt)

	if _, err := ReceiptStatus(runDir); err == nil || !strings.Contains(err.Error(), "does not match its content") {
		t.Fatalf("expected a tamper-detection error, got %v", err)
	}
}

func TestReceiptStatusDetectsTargetMismatchWithStoredLifecycle(t *testing.T) {
	runDir := approvedReceiptFixture(t)
	receipt := readStoredReceipt(t, runDir)
	receipt.Attempt = 2 // still inside the valid range, so structural validation accepts it
	digest, err := digestOfContent(receiptContent{SchemaVersion: receipt.SchemaVersion, Target: receipt.Target, Reviewer: receipt.Reviewer, Attempt: receipt.Attempt, MaxCorrections: receipt.MaxCorrections, CorrectionsUsed: receipt.CorrectionsUsed})
	if err != nil {
		t.Fatal(err)
	}
	receipt.ID = digest
	overwriteReceipt(t, runDir, receipt)

	if _, err := ReceiptStatus(runDir); err == nil || !strings.Contains(err.Error(), "does not match the current approved review lifecycle") {
		t.Fatalf("expected a lifecycle-mismatch error, got %v", err)
	}
}

func TestReceiptStatusDetectsCorruptedSnapshot(t *testing.T) {
	runDir := approvedReceiptFixture(t)
	receipt := readStoredReceipt(t, runDir)
	stored, err := snapshot.Load(filepath.Join(runDir, "snapshot"), receipt.Target.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(runDir, "snapshot", "blobs", "sha256", strings.TrimPrefix(stored.Manifest.Entries[0].Digest, "sha256:"))
	if err := os.Chmod(blob, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("corrupt"), 0o444); err != nil {
		t.Fatal(err)
	}

	if _, err := ReceiptStatus(runDir); err == nil || !strings.Contains(err.Error(), "no longer matches its digest") {
		t.Fatalf("expected a corrupt-snapshot error, got %v", err)
	}
}

func TestReceiptStatusRejectsAStaleCandidateWhenTheWorkflowRecordDrifts(t *testing.T) {
	runDir := approvedReceiptFixture(t)

	// workflow.json is not write-once like every other artifact in this
	// package (settle replaces it with a plain atomic rename); simulate it
	// drifting after the receipt was already issued, with the candidate and
	// snapshot bytes themselves untouched and still independently valid.
	path := filepath.Join(runDir, "workflow.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var outcome agentrouter.Outcome
	if err := json.Unmarshal(encoded, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Risk.Tier == risk.High {
		outcome.Risk.Tier = risk.Medium
	} else {
		outcome.Risk.Tier = risk.High
	}
	drifted, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, drifted, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReceiptStatus(runDir); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected a stale-candidate error, got %v", err)
	}
}

func TestReceiptStatusSkipsLiveWorkflowCheckForACorrectionReceipt(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceFor(runDir)
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n\n// fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrector := correctingProposal(workspace)
	if _, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: approvingReviewer(t, 2), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector}); err != nil {
		t.Fatal(err)
	}

	// The correction's revised candidate was never declared by workflow.json
	// — it is owned entirely by this package — so a receipt for attempt 2
	// must still verify even though consent.Status would resolve to the
	// original, pre-correction target.
	receipt, err := ReceiptStatus(runDir)
	if err != nil || receipt == nil {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
}

func TestReceiptStatusDetectsCorruptedCorrectionSnapshot(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceFor(runDir)
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n\n// fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrector := correctingProposal(workspace)
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: approvingReviewer(t, 2), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector})
	if err != nil {
		t.Fatal(err)
	}

	correctionSnapshotDir := filepath.Join(runDir, DirectoryName, CorrectionSnapshotDirectory)
	stored, err := snapshot.Load(correctionSnapshotDir, record.Target.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(correctionSnapshotDir, "blobs", "sha256", strings.TrimPrefix(stored.Manifest.Entries[0].Digest, "sha256:"))
	if err := os.Chmod(blob, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("corrupt"), 0o444); err != nil {
		t.Fatal(err)
	}

	if _, err := ReceiptStatus(runDir); err == nil || !strings.Contains(err.Error(), "no longer matches its digest") {
		t.Fatalf("expected a corrupt-snapshot error, got %v", err)
	}
}

func approvedReceiptFixture(t *testing.T) string {
	t.Helper()
	runDir, _ := reviewFixture(t, consent.Granted)
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		return boundResult(request, VerdictApproved), nil
	})
	if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err != nil {
		t.Fatal(err)
	}
	return runDir
}

func readStoredReceipt(t *testing.T, runDir string) Receipt {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(runDir, DirectoryName, ReceiptFileName))
	if err != nil {
		t.Fatal(err)
	}
	var receipt Receipt
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func overwriteReceipt(t *testing.T, runDir string, receipt Receipt) {
	t.Helper()
	path := filepath.Join(runDir, DirectoryName, ReceiptFileName)
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
}
