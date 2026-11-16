package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	reviewcore "github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/risk"
)

type fakeReviewer func(context.Context, reviewcore.Request, string) (reviewcore.Result, error)

func (function fakeReviewer) Review(ctx context.Context, request reviewcore.Request, snapshotDir string) (reviewcore.Result, error) {
	return function(ctx, request, snapshotDir)
}

type fakeCorrector func(context.Context, handoff.Request) (handoff.Result, error)

func (function fakeCorrector) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	return function(ctx, request)
}

// stubReviewProviders overrides the real OpenCode-backed providers for the
// duration of one test, restoring them afterward.
func stubReviewProviders(t *testing.T, reviewer reviewcore.Reviewer, corrector agentrouter.Runner) {
	t.Helper()
	previous := reviewProviders
	reviewProviders = func(string) (reviewcore.Reviewer, agentrouter.Runner, reviewcore.ReviewerIdentity, error) {
		return reviewer, corrector, reviewcore.ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"}, nil
	}
	t.Cleanup(func() { reviewProviders = previous })
}

func approvingFakeReviewer(t *testing.T, wantAttempt int) fakeReviewer {
	return func(_ context.Context, request reviewcore.Request, _ string) (reviewcore.Result, error) {
		if request.Attempt != wantAttempt {
			t.Fatalf("expected attempt %d, got %d", wantAttempt, request.Attempt)
		}
		return reviewcore.Result{SchemaVersion: reviewcore.CurrentSchemaVersion, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: reviewcore.VerdictApproved}, nil
	}
}

func pausingFakeReviewer(request reviewcore.Request) (reviewcore.Result, error) {
	return reviewcore.Result{
		SchemaVersion: reviewcore.CurrentSchemaVersion,
		RequestID:     request.ID,
		Attempt:       request.Attempt,
		Target:        request.Target,
		Verdict:       reviewcore.VerdictCorrectionRequired,
		Findings:      []reviewcore.Finding{{ID: "F1", Severity: reviewcore.SeverityBlocking, Summary: "Fix the boundary", Evidence: []reviewcore.Evidence{{URI: "repo://change.go", Digest: request.Snapshot.Entries[0].Digest}}}},
	}, nil
}

func (fixture consentCLIFixture) runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"review"}, args...), fixture.project, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestReviewStatusIsReadOnlyBeforeAnyAttempt(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	code, stdout, stderr := fixture.runCmd(t, "status", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "Review state: not started") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunRequiresGrantedConsent(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	reviewer := fakeReviewer(func(context.Context, reviewcore.Request, string) (reviewcore.Result, error) {
		t.Fatal("reviewer must not run without granted consent")
		return reviewcore.Result{}, nil
	})
	stubReviewProviders(t, reviewer, nil)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "granted") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunApprovesAfterGrantedConsent(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 0 {
		t.Fatalf("run exited with %d: %s", code, stderr)
	}
	for _, expected := range []string{"Review state: approved", "attempt: 1", "verdict: approved", "corrections used: 0/1", "receipt: sha256:", "mutation and delivery: not authorized"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("run output missing %q: %q", expected, stdout)
		}
	}

	statusCode, statusOut, statusErr := fixture.runCmd(t, "status", "--workflow", fixture.workflowID)
	if statusCode != 0 || !strings.Contains(statusOut, "Review state: approved") || !strings.Contains(statusOut, "receipt: sha256:") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", statusCode, statusOut, statusErr)
	}
}

func TestReviewApplyRequiresAVerifiedReceipt(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	before, err := os.ReadFile(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := fixture.runCmd(t, "apply", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "receipt") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	after, err := os.ReadFile(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused apply wrote to the workspace")
	}
}

func TestReviewApplyWritesTheReceiptedCandidateAndReportsInStatus(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
	if code, _, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID); code != 0 {
		t.Fatalf("run: %s", stderr)
	}

	code, stdout, stderr := fixture.runCmd(t, "apply", "--workflow", fixture.workflowID)
	if code != 0 {
		t.Fatalf("apply exited with %d: %s", code, stderr)
	}
	for _, expected := range []string{"Apply: applied", "workflow: " + fixture.workflowID, "delivery (commit, push, pull request, merge, release): not authorized"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("apply output missing %q: %q", expected, stdout)
		}
	}

	statusCode, statusOut, statusErr := fixture.runCmd(t, "status", "--workflow", fixture.workflowID)
	if statusCode != 0 || !strings.Contains(statusOut, "apply: applied") {
		t.Fatalf("status code=%d stdout=%q stderr=%q", statusCode, statusOut, statusErr)
	}
}

func TestReviewStatusOmitsReceiptForNonApprovedStates(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, fakeReviewer(func(_ context.Context, request reviewcore.Request, _ string) (reviewcore.Result, error) {
		return pausingFakeReviewer(request)
	}), nil)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "correction_required") || strings.Contains(stdout, "receipt:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunRequiresReviewModeOn(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	if code := Run([]string{"review", "mode", "disable"}, fixture.project, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("disable review mode exited with %d", code)
	}
	reviewer := fakeReviewer(func(context.Context, reviewcore.Request, string) (reviewcore.Result, error) {
		t.Fatal("reviewer must not run while review mode is off")
		return reviewcore.Result{}, nil
	})
	stubReviewProviders(t, reviewer, nil)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "mode") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunPausesForCorrectionAndRequiresSeparateDecision(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, fakeReviewer(func(_ context.Context, request reviewcore.Request, _ string) (reviewcore.Result, error) {
		return pausingFakeReviewer(request)
	}), nil)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "Review state: correction_required") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var secondRunCalls int
	stubReviewProviders(t, fakeReviewer(func(context.Context, reviewcore.Request, string) (reviewcore.Result, error) {
		secondRunCalls++
		return reviewcore.Result{}, nil
	}), nil)
	code, stdout, stderr = fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "separate correction decision") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if secondRunCalls != 0 {
		t.Fatal("provider ran before a correction decision was recorded")
	}
}

func TestReviewCorrectionGrantRunsOneCorrectionAndApproves(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, fakeReviewer(func(_ context.Context, request reviewcore.Request, _ string) (reviewcore.Result, error) {
		return pausingFakeReviewer(request)
	}), nil)
	if code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID); code != 0 || !strings.Contains(stdout, "correction_required") {
		t.Fatalf("pause code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	code, stdout, stderr := fixture.runCmd(t, "correction", "grant", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "Correction decision: granted") {
		t.Fatalf("grant correction code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	corrector := fakeCorrector(func(_ context.Context, request handoff.Request) (handoff.Result, error) {
		content, err := os.ReadFile(fixture.artifact)
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
		return handoff.Result{SchemaVersion: handoff.CurrentSchemaVersion, HandoffID: request.ID, Role: handoff.RoleExecutor, Status: handoff.StatusCompleted, Summary: "Applied the requested fix.", Artifacts: []handoff.Reference{{Kind: "source", URI: "repo://change.go", Digest: digest}}}, nil
	})
	stubReviewProviders(t, approvingFakeReviewer(t, 2), corrector)

	code, stdout, stderr = fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 0 {
		t.Fatalf("run correction exited with %d: %s", code, stderr)
	}
	for _, expected := range []string{"Review state: approved", "attempt: 2", "corrections used: 1/1"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("run output missing %q: %q", expected, stdout)
		}
	}
}

func TestReviewCorrectionDeclineSettlesRejectedWithoutRunningAProvider(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, fakeReviewer(func(_ context.Context, request reviewcore.Request, _ string) (reviewcore.Result, error) {
		return pausingFakeReviewer(request)
	}), nil)
	if code, _, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID); code != 0 {
		t.Fatalf("pause: %s", stderr)
	}

	if code, stdout, stderr := fixture.runCmd(t, "correction", "decline", "--workflow", fixture.workflowID); code != 0 || !strings.Contains(stdout, "Correction decision: declined") {
		t.Fatalf("decline correction code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var correctorCalls int
	corrector := fakeCorrector(func(context.Context, handoff.Request) (handoff.Result, error) {
		correctorCalls++
		return handoff.Result{}, nil
	})
	stubReviewProviders(t, fakeReviewer(func(context.Context, reviewcore.Request, string) (reviewcore.Result, error) {
		t.Fatal("reviewer must not run for a declined correction")
		return reviewcore.Result{}, nil
	}), corrector)

	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "Review state: rejected") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if correctorCalls != 0 {
		t.Fatal("corrector ran after correction was declined")
	}
}

func TestReviewStatusRejectsAStaleCandidateAfterApproval(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
	if code, _, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID); code != 0 {
		t.Fatalf("run: %s", stderr)
	}

	outcome := fixture.readOutcome(t)
	if outcome.Risk.Tier == risk.High {
		outcome.Risk.Tier = risk.Medium
	} else {
		outcome.Risk.Tier = risk.High
	}
	fixture.writeOutcome(t, outcome)

	code, stdout, stderr := fixture.runCmd(t, "status", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "stale") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunAlreadySettledReportsNothingToRun(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("grant consent: %s", stderr)
	}
	stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
	if code, _, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID); code != 0 {
		t.Fatalf("first run: %s", stderr)
	}

	var secondCalls int
	stubReviewProviders(t, fakeReviewer(func(context.Context, reviewcore.Request, string) (reviewcore.Result, error) {
		secondCalls++
		return reviewcore.Result{}, nil
	}), nil)
	code, stdout, stderr := fixture.runCmd(t, "run", "--workflow", fixture.workflowID)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "already settled") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if secondCalls != 0 {
		t.Fatal("provider ran for an already-settled review")
	}
}

func TestReviewCorrectionStatusIsReadOnly(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	code, stdout, stderr := fixture.runCmd(t, "correction", "status", "--workflow", fixture.workflowID)
	if code != 0 || !strings.Contains(stdout, "Correction decision: unset") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewRunRejectsMissingWorkflowAndUsageErrors(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	for _, args := range [][]string{
		{"review", "run"},
		{"review", "status"},
		{"review", "correction"},
		{"review", "apply"},
		{"review", "correction", "approve", "--workflow", fixture.workflowID},
		{"review", "run", "--workflow", fixture.workflowID, "extra"},
		{"review", "apply", "--workflow", fixture.workflowID, "extra"},
	} {
		var stderr bytes.Buffer
		if code := Run(args, fixture.project, &bytes.Buffer{}, &stderr); code != 2 || stderr.Len() == 0 {
			t.Errorf("%v: code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestHelpListsReviewRunStatusCorrection(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	if code := Run([]string{"help"}, t.TempDir(), &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("help exited with %d", code)
	}
	for _, expected := range []string{"review run", "review status", "review correction", "review apply"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help does not list %q: %q", expected, stdout.String())
		}
	}
}

var _ agentrouter.Runner = fakeCorrector(nil)
var _ reviewcore.Reviewer = fakeReviewer(nil)
