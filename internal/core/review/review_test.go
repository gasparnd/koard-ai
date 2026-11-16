package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/reviewmode"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

type reviewerFunc func(context.Context, Request, string) (Result, error)

func (function reviewerFunc) Review(ctx context.Context, request Request, snapshotDir string) (Result, error) {
	return function(ctx, request, snapshotDir)
}

type correctorFunc func(context.Context, handoff.Request) (handoff.Result, error)

func (function correctorFunc) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	return function(ctx, request)
}

func TestRunInitialApprovesExactConsentedSnapshot(t *testing.T) {
	runDir, target := reviewFixture(t, consent.Granted)
	identity := ReviewerIdentity{Runtime: "test", Model: "reviewer-v1", Agent: "independent"}
	before := readArtifact(t, runDir)
	var received Request
	reviewer := reviewerFunc(func(_ context.Context, request Request, snapshotDir string) (Result, error) {
		received = request
		if snapshotDir != filepath.Join(runDir, "snapshot") {
			t.Fatalf("unexpected snapshot directory %q", snapshotDir)
		}
		return boundResult(request, VerdictApproved), nil
	})

	record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: identity, ReviewMode: fixedMode(reviewmode.On)})
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if record.State != StateApproved || record.Target != target || record.MaxCorrections != 1 || record.CorrectionsUsed != 0 {
		t.Fatalf("unexpected lifecycle: %#v", record)
	}
	if received.Target != target || received.Reviewer != identity || received.Attempt != 1 {
		t.Fatalf("unexpected request: %#v", received)
	}
	if len(received.Capabilities) != 1 || received.Capabilities[0] != "repository:read" {
		t.Fatalf("review was not read-only: %#v", received.Capabilities)
	}
	if after := readArtifact(t, runDir); after != before {
		t.Fatalf("review changed workspace artifact: before=%q after=%q", before, after)
	}

	stored, err := Status(runDir)
	if err != nil || stored == nil || *stored != record {
		t.Fatalf("status=%#v err=%v", stored, err)
	}
	for _, name := range []string{"1-request.json", "1-result.json"} {
		if _, err := os.Stat(filepath.Join(runDir, DirectoryName, AttemptsDirectory, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestRunInitialPausesForSeparateCorrectionConsent(t *testing.T) {
	runDir, target := reviewFixture(t, consent.Granted)
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		result := boundResult(request, VerdictCorrectionRequired)
		result.Findings = []Finding{{ID: "F1", Severity: SeverityBlocking, Summary: "Fix the boundary", Evidence: []Evidence{{URI: "repo://change.go", Digest: request.Snapshot.Entries[0].Digest}}}}
		return result, nil
	})

	record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateCorrectionRequired || record.Target != target || record.CorrectionRequestID == "" || record.CorrectionsUsed != 0 {
		t.Fatalf("unexpected correction state: %#v", record)
	}
	request, err := CorrectionStatus(runDir)
	if err != nil || request == nil || request.ID != record.CorrectionRequestID || request.Target != target {
		t.Fatalf("correction request=%#v err=%v", request, err)
	}
}

func TestRunInitialRejectsWithoutGrantedExactConsent(t *testing.T) {
	for _, decision := range []consent.Decision{"", consent.Declined} {
		t.Run(string(decision), func(t *testing.T) {
			runDir, _ := reviewFixture(t, decision)
			var calls atomic.Int32
			reviewer := reviewerFunc(func(context.Context, Request, string) (Result, error) {
				calls.Add(1)
				return Result{}, nil
			})
			if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err == nil || !strings.Contains(err.Error(), "granted") {
				t.Fatalf("expected granted-consent error, got %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("reviewer ran without granted consent")
			}
			if _, err := os.Stat(filepath.Join(runDir, DirectoryName)); !os.IsNotExist(err) {
				t.Fatalf("refused review claimed state: %v", err)
			}
		})
	}
}

func TestRunInitialRejectsReviewModeOffBeforeClaim(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	var calls atomic.Int32
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		calls.Add(1)
		return boundResult(request, VerdictApproved), nil
	})
	if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.Off)}); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("expected review-mode error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("reviewer ran while review mode was off")
	}
	if _, err := os.Stat(filepath.Join(runDir, DirectoryName)); !os.IsNotExist(err) {
		t.Fatalf("mode-off review claimed state: %v", err)
	}
}

func TestRunInitialRejectsCorruptSnapshotBeforeClaim(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	target, _, err := consent.Status(runDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := snapshot.Load(filepath.Join(runDir, "snapshot"), target.SnapshotID)
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
	var calls atomic.Int32
	reviewer := reviewerFunc(func(context.Context, Request, string) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	})
	if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected snapshot verification error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("reviewer ran against a corrupt snapshot")
	}
	if _, err := os.Stat(filepath.Join(runDir, DirectoryName)); !os.IsNotExist(err) {
		t.Fatalf("corrupt snapshot claimed review state: %v", err)
	}
}

func TestRunInitialCancellationDoesNotClaimLifecycle(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunInitial(ctx, Options{RunDir: runDir, Reviewer: reviewerFunc(func(context.Context, Request, string) (Result, error) { return Result{}, nil }), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, DirectoryName)); !os.IsNotExist(err) {
		t.Fatalf("cancelled review claimed state: %v", err)
	}
}

func TestRunInitialCancellationAfterClaimFailsDiagnosably(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	ctx, cancel := context.WithCancel(context.Background())
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		cancel()
		return boundResult(request, VerdictApproved), nil
	})
	record, err := RunInitial(ctx, Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
	if err == nil || !errors.Is(err, context.Canceled) || record.State != StateFailed {
		t.Fatalf("record=%#v err=%v", record, err)
	}
	stored, statusErr := Status(runDir)
	if statusErr != nil || stored == nil || stored.State != StateFailed {
		t.Fatalf("stored=%#v err=%v", stored, statusErr)
	}
}

func TestStatusStrictlyDecodesLifecycle(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		return boundResult(request, VerdictApproved), nil
	})
	if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, DirectoryName, FileName)
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded[:len(encoded)-1], []byte(`,"extra":true}`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(runDir); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict decode error, got %v", err)
	}
}

func TestStatusRejectsImpossibleLifecycleRecords(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Lifecycle)
	}{
		{"approved without verdict or attempt", func(record *Lifecycle) { record.Attempt = 0; record.Verdict = "" }},
		{"invalid risk tier", func(record *Lifecycle) { record.Target.RiskTier = "impossible" }},
		{"failed without failure", func(record *Lifecycle) { record.State = StateFailed; record.Failure = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runDir, _ := reviewFixture(t, consent.Granted)
			reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
				return boundResult(request, VerdictApproved), nil
			})
			record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&record)
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(runDir, DirectoryName, FileName), encoded, 0o644); err != nil {
				t.Fatal(err)
			}
			if stored, err := Status(runDir); err == nil {
				t.Fatalf("accepted impossible lifecycle %#v", stored)
			}
		})
	}
}

func TestRunInitialReplayPreservesImmutableEvidence(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		return boundResult(request, VerdictApproved), nil
	})
	options := Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}
	if _, err := RunInitial(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(runDir, DirectoryName, FileName), filepath.Join(runDir, DirectoryName, AttemptsDirectory, "1-request.json"), filepath.Join(runDir, DirectoryName, AttemptsDirectory, "1-result.json")}
	before := make([][]byte, len(paths))
	for index, path := range paths {
		before[index], _ = os.ReadFile(path)
	}
	if _, err := RunInitial(context.Background(), options); err == nil {
		t.Fatal("replay unexpectedly succeeded")
	}
	for index, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before[index], after) {
			t.Fatalf("replay changed %s", path)
		}
	}
}

func TestRunInitialPersistsProviderAndValidationFailures(t *testing.T) {
	tests := []struct {
		name     string
		reviewer reviewerFunc
		want     string
	}{
		{"provider failure", func(context.Context, Request, string) (Result, error) {
			return Result{}, errors.New("provider unavailable")
		}, "provider unavailable"},
		{"wrong binding", func(_ context.Context, request Request, _ string) (Result, error) {
			result := boundResult(request, VerdictApproved)
			result.RequestID = "another-request"
			return result, nil
		}, "request_id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runDir, _ := reviewFixture(t, consent.Granted)
			record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: test.reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
			if err == nil || !strings.Contains(err.Error(), test.want) || record.State != StateFailed || !strings.Contains(record.Failure, test.want) {
				t.Fatalf("record=%#v err=%v", record, err)
			}
			stored, statusErr := Status(runDir)
			if statusErr != nil || stored == nil || stored.State != StateFailed {
				t.Fatalf("stored=%#v err=%v", stored, statusErr)
			}
		})
	}
}

func TestRunInitialClaimsLifecycleOnce(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	entered := make(chan struct{})
	release := make(chan struct{})
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		close(entered)
		<-release
		return boundResult(request, VerdictApproved), nil
	})
	options := Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		_, _ = RunInitial(context.Background(), options)
	}()
	<-entered
	if _, err := RunInitial(context.Background(), options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected claimed lifecycle error, got %v", err)
	}
	close(release)
	wait.Wait()
}

func fixedMode(mode reviewmode.Mode) func() (reviewmode.Mode, error) {
	return func() (reviewmode.Mode, error) { return mode, nil }
}

func TestRunInitialFailsClosedWhenReviewModeIsRevokedDuringProviderExecution(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	var calls atomic.Int32
	live := reviewmode.On
	check := func() (reviewmode.Mode, error) {
		current := live
		live = reviewmode.Off
		return current, nil
	}
	reviewer := reviewerFunc(func(context.Context, Request, string) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	})
	record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: check})
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("expected review-mode error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("reviewer ran after review mode authority was revoked")
	}
	if record.State != StateFailed {
		t.Fatalf("expected a failed lifecycle, got %#v", record)
	}
	stored, statusErr := Status(runDir)
	if statusErr != nil || stored == nil || stored.State != StateFailed {
		t.Fatalf("stored=%#v err=%v", stored, statusErr)
	}
}

func TestRunInitialClaimIsAtomicDespiteLeftoverStaging(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	// Simulates a process that crashed between staging and publishing a
	// previous claim attempt: the leftover staging directory must neither
	// block a fresh claim nor be mistaken for the published review directory.
	leftover := filepath.Join(runDir, ".review-claim-leftover")
	if err := os.Mkdir(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		return boundResult(request, VerdictApproved), nil
	})
	record, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)})
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if record.State != StateApproved {
		t.Fatalf("unexpected lifecycle: %#v", record)
	}
	reviewDir := filepath.Join(runDir, DirectoryName)
	if _, err := os.Stat(filepath.Join(reviewDir, FileName)); err != nil {
		t.Fatalf("published claim missing lifecycle file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reviewDir, AttemptsDirectory, "1-request.json")); err != nil {
		t.Fatalf("published claim missing attempts: %v", err)
	}
	if _, err := os.Stat(leftover); err != nil {
		t.Fatalf("unrelated leftover staging directory should be untouched: %v", err)
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".review-claim-") && entry.Name() != ".review-claim-leftover" {
			t.Fatalf("staging directory %q was not cleaned up after publish", entry.Name())
		}
	}
}

// pausedFixture drives a granted review to its correction_required pause and
// returns the run directory and the resulting correction request.
func pausedFixture(t *testing.T) (string, *CorrectionRequest) {
	t.Helper()
	runDir, _ := reviewFixture(t, consent.Granted)
	reviewer := reviewerFunc(func(_ context.Context, request Request, _ string) (Result, error) {
		result := boundResult(request, VerdictCorrectionRequired)
		result.Findings = []Finding{{ID: "F1", Severity: SeverityBlocking, Summary: "Fix the boundary", Evidence: []Evidence{{URI: "repo://change.go", Digest: request.Snapshot.Entries[0].Digest}}}}
		return result, nil
	})
	if _, err := RunInitial(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On)}); err != nil {
		t.Fatal(err)
	}
	request, err := CorrectionStatus(runDir)
	if err != nil || request == nil {
		t.Fatalf("correction request=%#v err=%v", request, err)
	}
	return runDir, request
}

// workspaceFor returns the workspace directory reviewFixture laid out next to
// runDir, so a correction test can propose a revision against real bytes.
func workspaceFor(runDir string) string {
	return filepath.Join(filepath.Dir(runDir), "workspace")
}

func approvingReviewer(t *testing.T, wantAttempt int) reviewerFunc {
	return func(_ context.Context, request Request, _ string) (Result, error) {
		if request.Attempt != wantAttempt {
			t.Fatalf("expected attempt %d, got %d", wantAttempt, request.Attempt)
		}
		return boundResult(request, VerdictApproved), nil
	}
}

func correctingProposal(workspace string) correctorFunc {
	return func(_ context.Context, request handoff.Request) (handoff.Result, error) {
		content, err := os.ReadFile(filepath.Join(workspace, "change.go"))
		if err != nil {
			return handoff.Result{}, err
		}
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
		return handoff.Result{SchemaVersion: handoff.CurrentSchemaVersion, HandoffID: request.ID, Role: handoff.RoleExecutor, Status: handoff.StatusCompleted, Summary: "Applied the requested fix.", Artifacts: []handoff.Reference{{Kind: "source", URI: "repo://change.go", Digest: digest}}}, nil
	}
}

func TestDecideCorrectionRequiresAPausedLifecycle(t *testing.T) {
	runDir, _ := reviewFixture(t, consent.Granted)
	if _, err := DecideCorrection(runDir, consent.Granted); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("expected a paused-lifecycle error, got %v", err)
	}
}

func TestDecideCorrectionIsIdempotentAndFailsClosedOnConflict(t *testing.T) {
	runDir, correctionRequest := pausedFixture(t)
	first, err := DecideCorrection(runDir, consent.Granted)
	if err != nil || first.Decision != consent.Granted || first.CorrectionRequestID != correctionRequest.ID {
		t.Fatalf("decision=%#v err=%v", first, err)
	}
	repeat, err := DecideCorrection(runDir, consent.Granted)
	if err != nil || repeat != first {
		t.Fatalf("repeat decision=%#v err=%v", repeat, err)
	}
	if _, err := DecideCorrection(runDir, consent.Declined); err == nil || !strings.Contains(err.Error(), "already records") {
		t.Fatalf("expected a conflicting-decision error, got %v", err)
	}
}

func TestRunCorrectionSettlesRejectedWhenDeclined(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Declined); err != nil {
		t.Fatal(err)
	}
	var correctorCalls atomic.Int32
	corrector := correctorFunc(func(context.Context, handoff.Request) (handoff.Result, error) {
		correctorCalls.Add(1)
		return handoff.Result{}, nil
	})
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: reviewerFunc(func(context.Context, Request, string) (Result, error) {
		t.Fatal("reviewer must not run for a declined correction")
		return Result{}, nil
	}), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspaceFor(runDir), Corrector: corrector})
	if err != nil {
		t.Fatalf("run correction: %v", err)
	}
	if record.State != StateRejected || record.Verdict != VerdictRejected || record.CorrectionRequestID != "" {
		t.Fatalf("unexpected lifecycle: %#v", record)
	}
	if correctorCalls.Load() != 0 {
		t.Fatal("corrector ran after correction was declined")
	}
}

func TestRunCorrectionRequiresASeparateDecision(t *testing.T) {
	runDir, _ := pausedFixture(t)
	var calls atomic.Int32
	corrector := correctorFunc(func(context.Context, handoff.Request) (handoff.Result, error) {
		calls.Add(1)
		return handoff.Result{}, nil
	})
	if _, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: reviewerFunc(func(context.Context, Request, string) (Result, error) { return Result{}, nil }), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspaceFor(runDir), Corrector: corrector}); err == nil || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("expected a missing-decision error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("corrector ran without a separate correction decision")
	}
}

func TestRunCorrectionRequiresReviewModeOnAndStaysRetryable(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	corrector := correctorFunc(func(context.Context, handoff.Request) (handoff.Result, error) {
		calls.Add(1)
		return handoff.Result{}, nil
	})
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: reviewerFunc(func(context.Context, Request, string) (Result, error) { return Result{}, nil }), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.Off), Workspace: workspaceFor(runDir), Corrector: corrector})
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("expected a review-mode error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("corrector ran while review mode was off")
	}
	if record.State != StateCorrectionRequired {
		t.Fatalf("a revoked attempt must stay retryable, got %#v", record)
	}
	stored, statusErr := Status(runDir)
	if statusErr != nil || stored == nil || stored.State != StateCorrectionRequired {
		t.Fatalf("persisted lifecycle must stay retryable: stored=%#v err=%v", stored, statusErr)
	}
}

func TestRunCorrectionApprovesRevisedCandidateAfterGrant(t *testing.T) {
	runDir, _ := pausedFixture(t)
	if _, err := DecideCorrection(runDir, consent.Granted); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceFor(runDir)
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n\n// fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var correctorCalls atomic.Int32
	corrector := correctorFunc(func(ctx context.Context, request handoff.Request) (handoff.Result, error) {
		correctorCalls.Add(1)
		return correctingProposal(workspace)(ctx, request)
	})
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: approvingReviewer(t, 2), ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector})
	if err != nil {
		t.Fatalf("run correction: %v", err)
	}
	if record.State != StateApproved || record.Attempt != 2 || record.CorrectionsUsed != 1 || record.CorrectionRequestID != "" {
		t.Fatalf("unexpected lifecycle: %#v", record)
	}
	if correctorCalls.Load() != 1 {
		t.Fatalf("expected exactly one corrector call, got %d", correctorCalls.Load())
	}
	stored, statusErr := Status(runDir)
	if statusErr != nil || stored == nil || *stored != record {
		t.Fatalf("stored=%#v err=%v", stored, statusErr)
	}
	for _, name := range []string{"2-request.json", "2-result.json"} {
		if _, err := os.Stat(filepath.Join(runDir, DirectoryName, AttemptsDirectory, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestRunCorrectionEscalatesOnSecondCorrectionRequired(t *testing.T) {
	runDir, originalRequest := pausedFixture(t)
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
	record, err := RunCorrection(context.Background(), Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, ReviewMode: fixedMode(reviewmode.On), Workspace: workspace, Corrector: corrector})
	if err != nil {
		t.Fatalf("run correction: %v", err)
	}
	if record.State != StateEscalated || record.Attempt != 2 || record.CorrectionsUsed != 1 {
		t.Fatalf("unexpected lifecycle: %#v", record)
	}
	// No second pause: the original correction request remains the only one
	// ever written, proving no loop was permitted.
	current, err := CorrectionStatus(runDir)
	if err != nil || current == nil || current.ID != originalRequest.ID {
		t.Fatalf("expected the original correction request to remain unchanged: current=%#v err=%v", current, err)
	}
}

func boundResult(request Request, verdict Verdict) Result {
	return Result{SchemaVersion: CurrentSchemaVersion, RequestID: request.ID, Target: request.Target, Attempt: request.Attempt, Verdict: verdict}
}

func reviewFixture(t *testing.T, decision consent.Decision) (string, consent.Target) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	runDir := filepath.Join(root, "workflow-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidateID := "sha256:" + strings.Repeat("a", 64)
	captured, err := snapshot.Capture(context.Background(), workspace, filepath.Join(runDir, "snapshot"), candidateID, []handoff.Reference{{Kind: "source", URI: "repo://change.go"}})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Assess(filepath.Join(runDir, "snapshot"), captured.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome := agentrouter.Outcome{SchemaVersion: agentrouter.CurrentSchemaVersion, WorkflowID: filepath.Base(runDir), TaskID: "task-1", Status: handoff.StatusCompleted, Candidate: &candidate.Candidate{SchemaVersion: candidate.CurrentSchemaVersion, ID: candidateID, TaskID: "task-1", HandoffID: "workflow-1-1", SnapshotID: captured.ID}, Risk: &assessment}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "workflow.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	target := consent.Target{WorkflowID: filepath.Base(runDir), CandidateID: candidateID, SnapshotID: captured.ID, RiskTier: assessment.Tier}
	if decision != "" {
		if _, err := consent.Decide(runDir, decision); err != nil {
			t.Fatal(err)
		}
	}
	return runDir, target
}

func readArtifact(t *testing.T, runDir string) string {
	t.Helper()
	stored, err := snapshot.Load(filepath.Join(runDir, "snapshot"), func() string {
		_, record, _ := consent.Status(runDir)
		if record != nil {
			return record.Target.SnapshotID
		}
		target, _, _ := consent.Status(runDir)
		return target.SnapshotID
	}())
	if err != nil {
		t.Fatal(err)
	}
	content, err := snapshot.ReadEntry(filepath.Join(runDir, "snapshot"), stored.Manifest.Entries[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
