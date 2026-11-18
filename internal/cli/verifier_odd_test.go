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

// Independent negative-evidence and read-only projection probes.
func TestVerifierODDEvidenceBoundaries(t *testing.T) {
	for _, kind := range []string{"claim-only", "missing-request", "trailing-preparation", "invented-completion", "unknown-mode", "cancelled", "failed"} {
		t.Run(kind, func(t *testing.T) {
			cwd, calls := oddFixture(t)
			if kind == "cancelled" || kind == "failed" {
				workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
					return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
						*calls++
						if kind == "cancelled" {
							return handoff.Result{}, context.Canceled
						}
						return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusFailed, Summary: "Cannot proceed"}, nil
					}), nil
				}
			}
			code, _, errText := memoryCommand(t, cwd, "odd", "run", "--workflow", "demo", "--task", "task", "--objective", "Bounded proposal")
			if kind == "cancelled" || kind == "failed" {
				if code != 1 || errText == "" {
					t.Fatalf("run %d %q", code, errText)
				}
			} else if code != 0 || errText != "" {
				t.Fatalf("run %d %q", code, errText)
			}
			dir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
			switch kind {
			case "claim-only":
				if err := os.Remove(filepath.Join(dir, "workflow.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-request":
				if err := os.Remove(filepath.Join(dir, "handoffs", "1-request.json")); err != nil {
					t.Fatal(err)
				}
			case "trailing-preparation", "invented-completion":
				path := filepath.Join(dir, "odd.json")
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "trailing-preparation" {
					b = append(b, []byte("{}")...)
				} else {
					b = []byte(strings.Replace(string(b), `"implement"`, `"completed"`, 1))
				}
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, n := verifierTree(t, cwd), *calls
			p, err := oddworkflow.Status(context.Background(), cwd, "demo", func() (reviewmode.Mode, error) {
				if kind == "unknown-mode" {
					return "unexpected", nil
				}
				return reviewmode.On, nil
			})
			switch kind {
			case "claim-only":
				if err != nil || p.Reason != "proposal_incomplete" || p.Machine.State != "blocked" {
					t.Fatalf("%+v %v", p, err)
				}
			case "cancelled", "failed":
				if err != nil || p.Reason != "proposal_failed" || p.Machine.State != "blocked" {
					t.Fatalf("%+v %v", p, err)
				}
			case "unknown-mode":
				if err == nil || err.Error() != "invalid review mode" {
					t.Fatal(err)
				}
			case "invented-completion":
				if err == nil || err.Error() != "invalid ODD preparation binding" {
					t.Fatal(err)
				}
			case "trailing-preparation":
				if err == nil || err.Error() != "trailing ODD JSON" {
					t.Fatal(err)
				}
			default:
				if err == nil {
					t.Fatal("missing evidence accepted")
				}
			}
			if before != verifierTree(t, cwd) || n != *calls {
				t.Fatal("status mutated state or called provider")
			}
		})
	}
}

func TestVerifierODDReceiptRemoval(t *testing.T) {
	cwd, calls := oddFixture(t)
	oddRun(t, cwd, "planned")
	oddAction(t, cwd, "review", "consent", "grant")
	stubReviewProviders(t, approvingFakeReviewer(t, 1), nil)
	oddAction(t, cwd, "review", "run")
	p, err := oddworkflow.Status(context.Background(), cwd, "demo", nil)
	if err != nil || p.Reason != "apply_required" {
		t.Fatalf("%+v %v", p, err)
	}
	oddAction(t, cwd, "review", "apply")
	p, err = oddworkflow.Status(context.Background(), cwd, "demo", nil)
	if err != nil || p.Reason != "applied" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := os.Remove(filepath.Join(cwd, ".koard", "state", "runs", "demo", "apply.json")); err != nil {
		t.Fatal(err)
	}
	before, n := verifierTree(t, cwd), *calls
	p, err = oddworkflow.Status(context.Background(), cwd, "demo", nil)
	if err != nil || p.Reason != "apply_required" || p.Machine.State == "completed" {
		t.Fatalf("%+v %v", p, err)
	}
	if before != verifierTree(t, cwd) || n != *calls {
		t.Fatal("projection mutated state")
	}
}

func TestVerifierODDActualCancellation(t *testing.T) {
	cwd, _ := oddFixture(t)
	b, err := os.ReadFile(filepath.Join(cwd, ".koard", "koard.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := oddworkflow.Prepare(context.Background(), cwd, "demo", "task", "Cancel proposal", "fixed", cfg)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(cwd, ".koard", "state", "runs")
	if err = os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	out, err := agentrouter.Route(ctx, agentrouter.Options{WorkflowID: "demo", TaskID: "task", Objective: "Cancel proposal", Policy: "fixed", StateDir: state, Workspace: cwd, Initialize: func(dir string) error { return p.Initialize(cwd, dir) }, Runner: workflowFake(func(ctx context.Context, r handoff.Request) (handoff.Result, error) {
		calls++
		cancel()
		return handoff.Result{}, ctx.Err()
	})})
	if !errors.Is(err, context.Canceled) || calls != 1 || out.Status != handoff.StatusFailed {
		t.Fatalf("%+v %v calls=%d", out, err, calls)
	}
	before := verifierTree(t, cwd)
	progress, err := oddworkflow.Status(context.Background(), cwd, "demo", nil)
	if err != nil || progress.Reason != "proposal_cancelled" || progress.Machine.State != "stopped" {
		t.Fatalf("%+v %v", progress, err)
	}
	oddStatus(t, cwd, "stopped", "proposal_cancelled")
	if before != verifierTree(t, cwd) || calls != 1 {
		t.Fatal("status side effects")
	}
}

func TestVerifierODDInvalidPreflight(t *testing.T) {
	cwd, calls := oddFixture(t)
	constructed := 0
	workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
		constructed++
		return nil, errors.New("must not construct")
	}
	before := verifierTree(t, cwd)
	c, o, e := memoryCommand(t, cwd, "odd", "run", "--workflow", "demo", "--task", "task", "--objective", "No state", "--policy", "unknown")
	want := "koard odd run: unsupported routing policy \"unknown\"; supported policies are \"fixed\" and \"planned\"\n"
	if c != 1 || o != "" || e != want {
		t.Fatalf("%d %q %q", c, o, e)
	}
	if before != verifierTree(t, cwd) || *calls != 0 || constructed != 0 {
		t.Fatal("preflight side effects")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".koard", "state", "runs", "demo")); !os.IsNotExist(err) {
		t.Fatalf("claimed state: %v", err)
	}
}

func TestVerifierODDCorrectedTarget(t *testing.T) {
	cwd, calls := oddFixture(t)
	oddRun(t, cwd, "fixed")
	oddAction(t, cwd, "review", "consent", "grant")
	stubReviewProviders(t, fakeReviewer(func(_ context.Context, r reviewcore.Request, _ string) (reviewcore.Result, error) {
		return pausingFakeReviewer(r)
	}), nil)
	oddAction(t, cwd, "review", "run")
	oddAction(t, cwd, "review", "correction", "grant")
	stubReviewProviders(t, approvingFakeReviewer(t, 2), fakeCorrector(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
		// Simulate revised source in this isolated fixture, never a real provider.
		if err := os.WriteFile(filepath.Join(cwd, "change.go"), []byte("package corrected\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Corrected", Artifacts: []handoff.Reference{{Kind: "source", URI: "repo://change.go"}}}, nil
	}))
	oddAction(t, cwd, "review", "run")
	dir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
	receipt, err := reviewcore.ReceiptStatus(dir)
	if err != nil || receipt == nil || receipt.Attempt != 2 {
		t.Fatalf("%+v %v", receipt, err)
	}
	p, err := oddworkflow.Status(context.Background(), cwd, "demo", nil)
	if err != nil || p.Target == nil || *p.Target != receipt.Target || p.Reason != "apply_required" {
		t.Fatalf("%+v %v", p, err)
	}
	oddStatus(t, cwd, "implement", "apply_required")
	oddAction(t, cwd, "review", "apply")
	oddStatus(t, cwd, "completed", "applied")
	path := filepath.Join(dir, "apply.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	record["target"].(map[string]any)["candidate_id"] = "sha256:" + strings.Repeat("0", 64)
	b, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	before := verifierTree(t, cwd)
	c, o, e := memoryCommand(t, cwd, "odd", "status", "--workflow", "demo")
	if c != 1 || o != "" || e != "koard odd: ODD apply target does not match the verified receipt\n" {
		t.Fatalf("%d %q %q", c, o, e)
	}
	if before != verifierTree(t, cwd) || *calls != 2 {
		t.Fatal("projection side effects")
	}
}

func TestVerifierODDLegacyRunnerCancellation(t *testing.T) {
	cwd := workflowProject(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := workflowProvider
	t.Cleanup(func() { workflowProvider = previous })
	workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
		return workflowFake(func(context.Context, handoff.Request) (handoff.Result, error) {
			return handoff.Result{}, context.Canceled
		}), nil
	}
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Bounded proposal")
	if c != 1 || o != "workflow: demo\npolicy: fixed\nstatus: failed\n" || e != "koard run: execute handoff \"demo-1\": context canceled\n" {
		t.Fatalf("%d %q %q", c, o, e)
	}
}
