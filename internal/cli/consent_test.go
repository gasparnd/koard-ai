package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

type consentCLIFixture struct {
	project    string
	configDir  string
	workflowID string
	runDir     string
	artifact   string
}

func newConsentCLIFixture(t *testing.T) consentCLIFixture {
	t.Helper()
	fixture := consentCLIFixture{
		project:    t.TempDir(),
		configDir:  t.TempDir(),
		workflowID: "workflow-1",
	}
	t.Setenv("KOARD_CONFIG_DIR", fixture.configDir)
	if code := Run([]string{"init"}, fixture.project, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("init exited with %d", code)
	}
	fixture.runDir = filepath.Join(fixture.project, ".koard", "state", "runs", fixture.workflowID)
	fixture.artifact = filepath.Join(fixture.project, "change.go")
	if err := os.MkdirAll(fixture.runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.artifact, []byte("package change\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	candidateID := "sha256:" + strings.Repeat("a", 64)
	captured, err := snapshot.Capture(context.Background(), fixture.project, filepath.Join(fixture.runDir, "snapshot"), candidateID, []handoff.Reference{{Kind: "source", URI: "repo://change.go"}})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := risk.Assess(filepath.Join(fixture.runDir, "snapshot"), captured.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome := agentrouter.Outcome{
		SchemaVersion: agentrouter.CurrentSchemaVersion,
		WorkflowID:    fixture.workflowID,
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
	if err := os.WriteFile(filepath.Join(fixture.runDir, "workflow.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture consentCLIFixture) run(t *testing.T, action string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"review", "consent", action, "--workflow", fixture.workflowID}, fixture.project, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestReviewConsentStatusIsReadOnly(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	code, stdout, stderr := fixture.run(t, "status")
	if code != 0 {
		t.Fatalf("status exited with %d: %s", code, stderr)
	}
	for _, expected := range []string{
		"Review consent: unset",
		"workflow: workflow-1",
		"candidate: sha256:",
		"snapshot: sha256:",
		"risk: medium",
		"authority: none",
		"mutation and delivery: not authorized",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("status output missing %q: %q", expected, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.runDir, consent.FileName)); !os.IsNotExist(err) {
		t.Fatalf("status wrote consent state: %v", err)
	}
}

func TestReviewConsentGrantAndDeclineBindExactWorkflow(t *testing.T) {
	for _, action := range []string{"grant", "decline"} {
		t.Run(action, func(t *testing.T) {
			fixture := newConsentCLIFixture(t)
			before, err := os.ReadFile(fixture.artifact)
			if err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := fixture.run(t, action)
			if code != 0 {
				t.Fatalf("%s exited with %d: %s", action, code, stderr)
			}
			decision := map[string]string{"grant": "granted", "decline": "declined"}[action]
			if !strings.Contains(stdout, "Review consent: "+decision) {
				t.Fatalf("unexpected output: %q", stdout)
			}
			if action == "grant" && !strings.Contains(stdout, "authority: independent review of this snapshot only") {
				t.Fatalf("grant did not bound authority: %q", stdout)
			}
			after, err := os.ReadFile(fixture.artifact)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("consent modified the candidate artifact")
			}
			if _, err := os.Stat(filepath.Join(fixture.runDir, consent.FileName)); err != nil {
				t.Fatalf("consent record missing: %v", err)
			}
		})
	}
}

func TestReviewConsentIsIdempotentAndImmutable(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := fixture.run(t, "grant"); code != 0 {
		t.Fatalf("idempotent grant exited with %d: %s", code, stderr)
	}
	if code, stdout, stderr := fixture.run(t, "decline"); code != 1 || stdout != "" || !strings.Contains(stderr, "already records") {
		t.Fatalf("conflict code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewConsentModeOffBlocksDecisionsButNotStatus(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	if code := Run([]string{"review", "mode", "disable"}, fixture.project, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("disable review mode exited with %d", code)
	}
	if code, _, stderr := fixture.run(t, "grant"); code != 1 || !strings.Contains(stderr, "review mode is off") {
		t.Fatalf("grant while off code=%d stderr=%q", code, stderr)
	}
	if code, _, stderr := fixture.run(t, "decline"); code != 1 || !strings.Contains(stderr, "review mode is off") {
		t.Fatalf("decline while off code=%d stderr=%q", code, stderr)
	}
	if code, stdout, stderr := fixture.run(t, "status"); code != 0 || !strings.Contains(stdout, "Review consent: unset") {
		t.Fatalf("status while off code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestReviewConsentFailsClosedWhileReviewStateIsLocked(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	lock := filepath.Join(fixture.configDir, reviewMutationLock)
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.run(t, "grant")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "operation is in progress") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(fixture.runDir, consent.FileName)); !os.IsNotExist(err) {
		t.Fatalf("locked operation wrote consent: %v", err)
	}
}

func TestReviewConsentRejectsMissingWorkflowAndUsageErrors(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	for _, args := range [][]string{
		{"review", "consent"},
		{"review", "consent", "approve", "--workflow", fixture.workflowID},
		{"review", "consent", "status"},
		{"review", "consent", "status", "--workflow"},
		{"review", "consent", "status", "--workflow", "../escape"},
		{"review", "consent", "status", "--workflow", fixture.workflowID, "extra"},
	} {
		var stderr bytes.Buffer
		if code := Run(args, fixture.project, &bytes.Buffer{}, &stderr); code != 2 || stderr.Len() == 0 {
			t.Errorf("%v: code=%d stderr=%q", args, code, stderr.String())
		}
	}

	var stderr bytes.Buffer
	code := Run([]string{"review", "consent", "status", "--workflow", "missing"}, fixture.project, &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "workflow.json") {
		t.Fatalf("missing workflow code=%d stderr=%q", code, stderr.String())
	}
}

func TestReviewConsentRejectsDuplicateWorkflowFlagWhenFirstValueIsEmpty(t *testing.T) {
	fixture := newConsentCLIFixture(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"review", "consent", "status",
		"--workflow=", "--workflow", fixture.workflowID,
	}, fixture.project, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--workflow may be specified only once") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestReviewConsentPublicBoundaryRejectsInvalidTargetState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, fixture consentCLIFixture)
		want   string
	}{
		{"incomplete workflow", func(t *testing.T, fixture consentCLIFixture) {
			outcome := fixture.readOutcome(t)
			outcome.Status = handoff.StatusBlocked
			fixture.writeOutcome(t, outcome)
		}, "is not completed"},
		{"risk binding", func(t *testing.T, fixture consentCLIFixture) {
			outcome := fixture.readOutcome(t)
			outcome.Risk.SnapshotID = "sha256:" + strings.Repeat("f", 64)
			fixture.writeOutcome(t, outcome)
		}, "risk snapshot_id"},
		{"corrupt snapshot blob", func(t *testing.T, fixture consentCLIFixture) {
			manifestPath := filepath.Join(fixture.runDir, "snapshot", "manifest.json")
			encoded, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var manifest snapshot.Manifest
			if err := json.Unmarshal(encoded, &manifest); err != nil {
				t.Fatal(err)
			}
			blob := filepath.Join(fixture.runDir, "snapshot", "blobs", "sha256", strings.TrimPrefix(manifest.Entries[0].Digest, "sha256:"))
			if err := os.Chmod(blob, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(blob, []byte("corrupt"), 0o444); err != nil {
				t.Fatal(err)
			}
		}, "no longer matches its digest"},
	}

	for _, test := range tests {
		for _, action := range []string{"grant", "decline"} {
			t.Run(test.name+"/"+action, func(t *testing.T) {
				fixture := newConsentCLIFixture(t)
				test.mutate(t, fixture)
				code, stdout, stderr := fixture.run(t, action)
				if code != 1 || stdout != "" || !strings.Contains(stderr, test.want) {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				if _, err := os.Stat(filepath.Join(fixture.runDir, consent.FileName)); !os.IsNotExist(err) {
					t.Fatalf("invalid target wrote consent: %v", err)
				}
			})
		}
	}
}

func (fixture consentCLIFixture) readOutcome(t *testing.T) agentrouter.Outcome {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(fixture.runDir, "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var outcome agentrouter.Outcome
	if err := json.Unmarshal(encoded, &outcome); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func (fixture consentCLIFixture) writeOutcome(t *testing.T, outcome agentrouter.Outcome) {
	t.Helper()
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.runDir, "workflow.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHelpListsReviewConsent(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	if code := Run([]string{"help"}, t.TempDir(), &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("help exited with %d", code)
	}
	if !strings.Contains(stdout.String(), "review consent") {
		t.Fatalf("help does not list review consent: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  review mode <status|enable|disable>") || strings.Contains(stdout.String(), "\treview mode") {
		t.Fatalf("help changed the existing review-mode indentation: %q", stdout.String())
	}
}
