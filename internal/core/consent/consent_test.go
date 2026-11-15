package consent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/candidate"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

func TestDecideBindsVerifiedTargetAndIsIdempotent(t *testing.T) {
	runDir, target := consentFixture(t)

	first, err := Decide(runDir, Granted)
	if err != nil {
		t.Fatalf("grant consent: %v", err)
	}
	second, err := Decide(runDir, Granted)
	if err != nil {
		t.Fatalf("repeat consent: %v", err)
	}
	if first != second {
		t.Fatalf("idempotent decision changed: %#v != %#v", first, second)
	}
	if first.Target != target || first.Decision != Granted || first.Authority != Review {
		t.Fatalf("unexpected consent record: %#v", first)
	}

	statusTarget, stored, err := Status(runDir)
	if err != nil {
		t.Fatalf("read consent status: %v", err)
	}
	if statusTarget != target || stored == nil || *stored != first {
		t.Fatalf("unexpected status target=%#v record=%#v", statusTarget, stored)
	}
}

func TestDecideDeclineIsCandidateScoped(t *testing.T) {
	runDir, target := consentFixture(t)
	record, err := Decide(runDir, Declined)
	if err != nil {
		t.Fatalf("decline consent: %v", err)
	}
	if record.Decision != Declined || record.Target != target {
		t.Fatalf("unexpected declined consent: %#v", record)
	}
}

func TestDecideRejectsConflictingDecision(t *testing.T) {
	runDir, _ := consentFixture(t)
	if _, err := Decide(runDir, Declined); err != nil {
		t.Fatal(err)
	}
	if _, err := Decide(runDir, Granted); err == nil || !strings.Contains(err.Error(), "already records") {
		t.Fatalf("expected immutable decision error, got %v", err)
	}
}

func TestConcurrentConflictingDecisionsPublishOneImmutableWinner(t *testing.T) {
	runDir, _ := consentFixture(t)
	const workers = 40
	start := make(chan struct{})
	results := make(chan error, workers)
	var wait sync.WaitGroup

	for index := 0; index < workers; index++ {
		decision := Granted
		if index%2 != 0 {
			decision = Declined
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := Decide(runDir, decision)
			results <- err
		}()
	}

	close(start)
	wait.Wait()
	close(results)

	_, record, err := Status(runDir)
	if err != nil || record == nil {
		t.Fatalf("status: record=%#v err=%v", record, err)
	}
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != workers/2 {
		t.Fatalf("got %d successful decisions; want %d", successes, workers/2)
	}
}

func TestStatusRejectsUnboundOrUnverifiableWorkflow(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, runDir string, outcome *agentrouter.Outcome)
		want   string
	}{
		{"workflow identity", func(t *testing.T, _ string, outcome *agentrouter.Outcome) {
			outcome.WorkflowID = "another-workflow"
		}, "does not match run directory"},
		{"missing candidate", func(t *testing.T, _ string, outcome *agentrouter.Outcome) {
			outcome.Candidate = nil
		}, "has no candidate"},
		{"risk binding", func(t *testing.T, _ string, outcome *agentrouter.Outcome) {
			outcome.Risk.SnapshotID = "sha256:" + strings.Repeat("f", 64)
		}, "risk snapshot_id"},
		{"snapshot candidate binding", func(t *testing.T, runDir string, outcome *agentrouter.Outcome) {
			manifestPath := filepath.Join(runDir, "snapshot", "manifest.json")
			encoded, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			encoded = []byte(strings.Replace(string(encoded), outcome.Candidate.ID, "sha256:"+strings.Repeat("e", 64), 1))
			if err := os.Chmod(manifestPath, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, encoded, 0o444); err != nil {
				t.Fatal(err)
			}
		}, "snapshot manifest has identity"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runDir, _ := consentFixture(t)
			outcome := readOutcome(t, runDir)
			test.mutate(t, runDir, &outcome)
			writeOutcome(t, runDir, outcome)
			if _, _, err := Status(runDir); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestStatusRejectsInvalidConsentRecord(t *testing.T) {
	runDir, target := consentFixture(t)
	valid := Record{SchemaVersion: CurrentSchemaVersion, Target: target, Decision: Granted, Authority: Review}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"unknown field", strings.TrimSuffix(string(encoded), "}") + `,"extra":true}`, "unknown field"},
		{"trailing value", string(encoded) + `{}`, "multiple JSON values"},
		{"schema", strings.Replace(string(encoded), `"schema_version":1`, `"schema_version":2`, 1), "not supported"},
		{"authority", strings.Replace(string(encoded), `"authority":"review"`, `"authority":"apply"`, 1), "authority"},
		{"target", strings.Replace(string(encoded), target.CandidateID, "sha256:"+strings.Repeat("d", 64), 1), "does not match"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(runDir, FileName), []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Status(runDir); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func consentFixture(t *testing.T) (string, Target) {
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
	writeOutcome(t, runDir, outcome)
	return runDir, Target{
		WorkflowID:  filepath.Base(runDir),
		CandidateID: candidateID,
		SnapshotID:  captured.ID,
		RiskTier:    assessment.Tier,
	}
}

func readOutcome(t *testing.T, runDir string) agentrouter.Outcome {
	t.Helper()
	var outcome agentrouter.Outcome
	encoded, err := os.ReadFile(filepath.Join(runDir, "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &outcome); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func writeOutcome(t *testing.T, runDir string, outcome agentrouter.Outcome) {
	t.Helper()
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "workflow.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}
