package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/application/oddworkflow"
	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	reviewcore "github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/reviewmode"
)

func oddFixture(t *testing.T) (string, *int) {
	t.Helper()
	cwd := workflowProject(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(cwd, "change.go"), []byte("package example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := workflowProvider
	t.Cleanup(func() { workflowProvider = previous })
	calls := new(int)
	workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
		return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
			*calls++
			out := handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Read-only proposal"}
			if r.To == handoff.RoleExecutor {
				out.Artifacts = []handoff.Reference{{Kind: "source", URI: "repo://change.go"}}
			}
			return out, nil
		}), nil
	}
	return cwd, calls
}

func oddRun(t *testing.T, cwd, policy string) string {
	t.Helper()
	c, o, e := memoryCommand(t, cwd, "odd", "run", "--workflow", "demo", "--task", "task", "--objective", "Propose a change", "--policy", policy)
	if c != 0 || e != "" {
		t.Fatalf("odd run: %d %s %s", c, o, e)
	}
	return o
}

func oddStatus(t *testing.T, cwd, state, reason string) string {
	t.Helper()
	before := verifierTree(t, cwd)
	c, o, e := memoryCommand(t, cwd, "odd", "status", "--workflow", "demo")
	if c != 0 || e != "" || !strings.Contains(o, "ODD state: "+state) || !strings.Contains(o, "reason: "+reason) {
		t.Fatalf("odd status: %d %s %s", c, o, e)
	}
	if before != verifierTree(t, cwd) {
		t.Fatal("status changed repository state")
	}
	return o
}

func TestODDGuidedApply(t *testing.T) {
	for _, policy := range []string{"fixed", "planned"} {
		t.Run(policy, func(t *testing.T) {
			cwd, calls := oddFixture(t)
			o := oddRun(t, cwd, policy)
			if !strings.Contains(o, "ODD state: blocked") || !strings.Contains(o, "reason: consent_required") {
				t.Fatal(o)
			}
			want := 2
			if policy == "planned" {
				want = 3
			}
			if *calls != want {
				t.Fatal(*calls)
			}
			oddStatus(t, cwd, "blocked", "consent_required")
			runDir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
			if _, err := os.Stat(filepath.Join(runDir, "odd.json")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(runDir, "consent.json")); !os.IsNotExist(err) {
				t.Fatal("ODD granted consent")
			}
			for _, args := range [][]string{{"review", "consent", "grant", "--workflow", "demo"}, {"review", "run", "--workflow", "demo"}} {
				stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
				if c, o, e := memoryCommand(t, cwd, args...); c != 0 || e != "" {
					t.Fatalf("%d %s %s", c, o, e)
				}
			}
			oddStatus(t, cwd, "implement", "apply_required")
			if err := os.WriteFile(filepath.Join(cwd, "change.go"), []byte("drift\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if c, o, e := memoryCommand(t, cwd, "review", "apply", "--workflow", "demo"); c != 0 || e != "" {
				t.Fatalf("apply %d %s %s", c, o, e)
			}
			oddStatus(t, cwd, "completed", "applied")
			body, err := os.ReadFile(filepath.Join(cwd, "change.go"))
			if err != nil || string(body) != "package example\n" {
				t.Fatalf("%q %v", body, err)
			}
			if *calls != want {
				t.Fatal("status/continuation reran proposal")
			}
			before := verifierTree(t, cwd)
			if c, _, _ := memoryCommand(t, cwd, "odd", "run", "--workflow", "demo", "--task", "task", "--objective", "Again"); c == 0 {
				t.Fatal("replayed run")
			}
			if *calls != want || before != verifierTree(t, cwd) {
				t.Fatal("replay changed state")
			}
		})
	}
}

func TestODDDecisionsAndMode(t *testing.T) {
	for _, mode := range []string{"disabled", "declined", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			cwd, _ := oddFixture(t)
			oddRun(t, cwd, "fixed")
			switch mode {
			case "disabled":
				if c, _, e := memoryCommand(t, cwd, "review", "mode", "disable", "--scope", "clone"); c != 0 {
					t.Fatal(e)
				}
				oddStatus(t, cwd, "blocked", "review_disabled")
			case "declined":
				if c, _, e := memoryCommand(t, cwd, "review", "consent", "decline", "--workflow", "demo"); c != 0 {
					t.Fatal(e)
				}
				oddStatus(t, cwd, "stopped", "consent_declined")
			case "rejected":
				stubReviewProviders(t, fakeReviewer(func(_ context.Context, r reviewcore.Request, _ string) (reviewcore.Result, error) {
					return reviewcore.Result{SchemaVersion: 1, RequestID: r.ID, Attempt: r.Attempt, Target: r.Target, Verdict: reviewcore.VerdictRejected}, nil
				}), nil)
				for _, action := range []string{"consent", "run"} {
					args := []string{"review", action}
					if action == "consent" {
						args = append(args, "grant")
					}
					args = append(args, "--workflow", "demo")
					if c, _, e := memoryCommand(t, cwd, args...); c != 0 {
						t.Fatal(e)
					}
				}
				oddStatus(t, cwd, "stopped", "review_rejected")
			}
		})
	}
}

func TestODDLegacyAndMalformedInputs(t *testing.T) {
	cwd, calls := oddFixture(t)
	for _, args := range [][]string{{"status"}, {"status", "--workflow", "../escape"}, {"status", "--workflow", "demo", "--workflow", "demo"}, {"run", "--workflow", "demo", "--task", "task"}} {
		before := verifierTree(t, cwd)
		if c, _, _ := memoryCommand(t, cwd, append([]string{"odd"}, args...)...); c == 0 {
			t.Fatal(args)
		}
		if *calls != 0 || before != verifierTree(t, cwd) {
			t.Fatal("invalid arguments changed state")
		}
	}
	if c, _, e := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Legacy"); c != 0 {
		t.Fatal(e)
	}
	before := verifierTree(t, cwd)
	if c, _, e := memoryCommand(t, cwd, "odd", "status", "--workflow", "demo"); c != 1 || !strings.Contains(e, "not an ODD run") {
		t.Fatalf("%d %s", c, e)
	}
	if before != verifierTree(t, cwd) {
		t.Fatal("legacy query created ODD state")
	}
}

func oddAction(t *testing.T, cwd string, args ...string) {
	t.Helper()
	c, o, e := memoryCommand(t, cwd, append(args, "--workflow", "demo")...)
	if c != 0 || e != "" {
		t.Fatalf("%v: %d %s %s", args, c, o, e)
	}
}

func TestODDCorrectionLifecycle(t *testing.T) {
	for _, decision := range []string{"grant", "decline", "escalate"} {
		t.Run(decision, func(t *testing.T) {
			cwd, calls := oddFixture(t)
			oddRun(t, cwd, "fixed")
			oddAction(t, cwd, "review", "consent", "grant")
			oddStatus(t, cwd, "implement", "review_ready")
			reviews, corrections := 0, 0
			stubReviewProviders(t, fakeReviewer(func(_ context.Context, r reviewcore.Request, _ string) (reviewcore.Result, error) {
				reviews++
				return pausingFakeReviewer(r)
			}), nil)
			oddAction(t, cwd, "review", "run")
			oddStatus(t, cwd, "blocked", "correction_consent_required")
			answer := decision
			if answer == "escalate" {
				answer = "grant"
			}
			oddAction(t, cwd, "review", "correction", answer)
			if answer == "decline" {
				oddStatus(t, cwd, "stopped", "correction_declined")
			} else {
				oddStatus(t, cwd, "implement", "correction_ready")
			}
			stubReviewProviders(t, fakeReviewer(func(_ context.Context, r reviewcore.Request, _ string) (reviewcore.Result, error) {
				reviews++
				if decision == "escalate" {
					return pausingFakeReviewer(r)
				}
				return approvingFakeReviewer(t, 2)(context.Background(), r, "")
			}), fakeCorrector(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
				corrections++
				return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Corrected candidate", Artifacts: []handoff.Reference{{Kind: "source", URI: "repo://change.go"}}}, nil
			}))
			oddAction(t, cwd, "review", "run")
			switch decision {
			case "decline":
				oddStatus(t, cwd, "stopped", "review_rejected")
				if reviews != 1 || corrections != 0 {
					t.Fatal("decline invoked provider")
				}
			case "escalate":
				oddStatus(t, cwd, "blocked", "review_escalated")
				if reviews != 2 || corrections != 1 {
					t.Fatal("correction budget")
				}
			case "grant":
				o := oddStatus(t, cwd, "implement", "apply_required")
				runDir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
				receipt, err := reviewcore.ReceiptStatus(runDir)
				if err != nil || receipt == nil || receipt.Attempt != 2 {
					t.Fatalf("%+v %v", receipt, err)
				}
				if !strings.Contains(o, receipt.Target.CandidateID) {
					t.Fatal("ODD uses initial rather than corrected target")
				}
				// Existing apply remains available after mode is disabled: ODD must
				// not invent a new gate for a previously verified receipt.
				if c, _, e := memoryCommand(t, cwd, "review", "mode", "disable", "--scope", "clone"); c != 0 {
					t.Fatal(e)
				}
				oddStatus(t, cwd, "implement", "apply_required")
				oddAction(t, cwd, "review", "apply")
				oddStatus(t, cwd, "completed", "applied")
			}
			if *calls != 2 {
				t.Fatal("ODD reran original workflow")
			}
		})
	}
}

func TestODDEvidenceAndModeErrors(t *testing.T) {
	for _, which := range []string{"mode", "preparation", "handoff", "apply"} {
		t.Run(which, func(t *testing.T) {
			cwd, _ := oddFixture(t)
			oddRun(t, cwd, "fixed")
			dir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
			if which == "mode" {
				_, err := oddworkflow.Status(context.Background(), cwd, "demo", func() (reviewmode.Mode, error) { return "", errors.New("corrupt mode") })
				if err == nil || !strings.Contains(err.Error(), "corrupt mode") {
					t.Fatalf("mode error hidden: %v", err)
				}
				return
			}
			switch which {
			case "preparation":
				if err := os.WriteFile(filepath.Join(dir, "odd.json"), []byte(`{"schema_version":999}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "handoff":
				if err := os.WriteFile(filepath.Join(dir, "handoffs", "1-result.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "apply":
				oddAction(t, cwd, "review", "consent", "grant")
				stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
				oddAction(t, cwd, "review", "run")
				oddAction(t, cwd, "review", "apply")
				path := filepath.Join(dir, "apply.json")
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var record map[string]any
				if err := json.Unmarshal(body, &record); err != nil {
					t.Fatal(err)
				}
				record["target"].(map[string]any)["candidate_id"] = "sha256:" + strings.Repeat("0", 64)
				body, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := verifierTree(t, cwd)
			c, o, e := memoryCommand(t, cwd, "odd", "status", "--workflow", "demo")
			if c != 1 || o != "" || e == "" {
				t.Fatalf("invalid evidence accepted %d %s %s", c, o, e)
			}
			if before != verifierTree(t, cwd) {
				t.Fatal("refusal changed evidence")
			}
		})
	}
}

func TestODDProposalFailure(t *testing.T) {
	for _, status := range []handoff.Status{handoff.StatusBlocked, handoff.StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			cwd, calls := oddFixture(t)
			workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
				return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
					*calls++
					return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: status, Summary: "Cannot proceed"}, nil
				}), nil
			}
			c, o, e := memoryCommand(t, cwd, "odd", "run", "--workflow", "demo", "--task", "task", "--objective", "Blocked")
			if c != 1 || e == "" || !strings.Contains(o, "ODD state: blocked") || *calls != 1 {
				t.Fatalf("%d %s %s calls=%d", c, o, e, *calls)
			}
			oddStatus(t, cwd, "blocked", "proposal_"+string(status))
		})
	}
}
