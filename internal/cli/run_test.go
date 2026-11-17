package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
)

type workflowFake func(context.Context, handoff.Request) (handoff.Result, error)

func (f workflowFake) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	return f(ctx, request)
}

func workflowProject(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	if code := Run([]string{"init"}, cwd, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal(code)
	}
	text := strings.ReplaceAll(defaultConfig, "provider: unset", "provider: codex")
	text = strings.ReplaceAll(text, "model: unset", "model: test-model")
	text = strings.Replace(text, "executor:\n    provider: codex", "executor:\n    provider: opencode", 1)
	for _, role := range []string{"planner", "executor", "verifier"} {
		provider := "codex"
		if role == "executor" {
			provider = "opencode"
		}
		text = strings.Replace(text, role+":\n    provider: "+provider+"\n    model: test-model", role+":\n    provider: "+provider+"\n    model: "+role, 1)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".koard", "koard.yaml"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestRunWorkflowPolicies(t *testing.T) {
	for _, policy := range []string{"fixed", "planned"} {
		t.Run(policy, func(t *testing.T) {
			cwd := workflowProject(t)
			original := workflowProvider
			t.Cleanup(func() { workflowProvider = original })
			var roles []handoff.Role
			var providers []string
			workflowProvider = func(_ string, cfg config.AgentConfig) (agentrouter.Runner, error) {
				providers = append(providers, cfg.Provider)
				return workflowFake(func(_ context.Context, request handoff.Request) (handoff.Result, error) {
					if cfg.Model != string(request.To) {
						t.Fatalf("model %q routed to %q", cfg.Model, request.To)
					}
					roles = append(roles, request.To)
					if !reflect.DeepEqual(request.Capabilities, []string{"repository:read"}) {
						t.Fatal("permissions widened")
					}
					if len(request.Context) > 0 {
						uri := request.Context[0].URI
						if !strings.HasPrefix(uri, "repo://.koard/state/runs/demo/handoffs/") {
							t.Fatalf("unresolvable context: %s", uri)
						}
						data, err := os.ReadFile(filepath.Join(cwd, strings.TrimPrefix(uri, "repo://")))
						if err != nil {
							t.Fatal(err)
						}
						if request.Context[0].Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(data)) {
							t.Fatal("context digest does not bind provider-visible bytes")
						}
					}
					return handoff.Result{SchemaVersion: 1, HandoffID: request.ID, Role: request.To, Status: handoff.StatusCompleted, Summary: "Read-only proposal"}, nil
				}), nil
			}
			args := []string{"run", "--workflow", "demo", "--task", "task", "--objective", "Plan a change", "--policy", policy}
			if policy == "fixed" {
				args = args[:len(args)-2]
			}
			var stdout, stderr bytes.Buffer
			if code := Run(args, cwd, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d err=%s", code, &stderr)
			}
			wantRoles, wantProviders := []handoff.Role{handoff.RoleExecutor, handoff.RoleVerifier}, []string{"opencode", "codex"}
			if policy == "planned" {
				wantRoles = append([]handoff.Role{handoff.RolePlanner}, wantRoles...)
				wantProviders = append([]string{"codex"}, wantProviders...)
			}
			if !reflect.DeepEqual(roles, wantRoles) || !reflect.DeepEqual(providers, wantProviders) {
				t.Fatalf("roles=%v providers=%v", roles, providers)
			}
			if !strings.Contains(stdout.String(), "workflow: demo\npolicy: "+policy+"\nstatus: completed\n") {
				t.Fatalf("output=%s", &stdout)
			}
			runDir := filepath.Join(cwd, ".koard", "state", "runs", "demo")
			// A completed routed candidate remains eligible for the separate review path.
			if _, _, err := consent.Status(runDir); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"consent.json", "review", "apply.json"} {
				if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
					t.Fatalf("unauthorized side effect: %s", name)
				}
			}
			stdout.Reset()
			stderr.Reset()
			before, err := os.ReadFile(filepath.Join(runDir, "workflow.json"))
			if err != nil {
				t.Fatal(err)
			}
			if code := Run(args, cwd, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "already exists") || len(roles) != len(wantRoles) {
				t.Fatalf("replay: %d %s", code, &stderr)
			}
			after, err := os.ReadFile(filepath.Join(runDir, "workflow.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("replay changed persisted evidence")
			}
		})
	}
}

func TestRunWorkflowRefusesInvalidInputBeforeExecution(t *testing.T) {
	for _, extra := range [][]string{{"--policy", "unknown"}, {"--workflow", "../escape"}, {"--task", ""}, {"--objective", ""}, {"--unknown"}, {"extra"}} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			cwd := workflowProject(t)
			args := append([]string{"run", "--workflow", "demo", "--task", "task", "--objective", "Goal"}, extra...)
			var stdout, stderr bytes.Buffer
			if code := Run(args, cwd, &stdout, &stderr); code == 0 || stderr.Len() == 0 || stdout.Len() != 0 {
				t.Fatalf("code=%d out=%s err=%s", code, &stdout, &stderr)
			}
			if _, err := os.Stat(filepath.Join(cwd, ".koard", "state", "runs")); !os.IsNotExist(err) {
				t.Fatal("invalid input created runs")
			}
		})
	}
}

func TestRunWorkflowFailureIsPersisted(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "provider failure"}[fail], func(t *testing.T) {
			cwd := workflowProject(t)
			original := workflowProvider
			t.Cleanup(func() { workflowProvider = original })
			workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
				return workflowFake(func(_ context.Context, req handoff.Request) (handoff.Result, error) {
					if fail {
						return handoff.Result{}, errors.New("provider unavailable")
					}
					return handoff.Result{SchemaVersion: 1, HandoffID: req.ID, Role: req.To, Status: handoff.StatusBlocked, Summary: "Need clarification"}, nil
				}), nil
			}
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"run", "--workflow", "demo", "--task", "task", "--objective", "Goal", "--policy", "planned"}, cwd, &stdout, &stderr); code != 1 {
				t.Fatalf("code=%d", code)
			}
			data, err := os.ReadFile(filepath.Join(cwd, ".koard", "state", "runs", "demo", "workflow.json"))
			if err != nil {
				t.Fatal(err)
			}
			var outcome agentrouter.Outcome
			if err := json.Unmarshal(data, &outcome); err != nil {
				t.Fatal(err)
			}
			want := handoff.StatusBlocked
			if fail {
				want = handoff.StatusFailed
			}
			if outcome.Status != want || outcome.Candidate != nil {
				t.Fatalf("outcome=%#v", outcome)
			}
		})
	}
}

func TestRunWorkflowHelpAndUnconfiguredProviders(t *testing.T) {
	cwd := workflowProject(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"run", "--help"}, cwd, &out, &errOut); code != 0 || !strings.Contains(out.String(), "--policy") || errOut.Len() != 0 {
		t.Fatalf("help: %d %s %s", code, &out, &errOut)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".koard", "koard.yaml"), []byte(defaultConfig), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run([]string{"run", "--workflow", "demo", "--task", "task", "--objective", "Goal"}, cwd, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "agents.executor.provider") {
		t.Fatalf("unset: %d %s", code, &errOut)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".koard", "state", "runs")); !os.IsNotExist(err) {
		t.Fatal("unconfigured provider created state")
	}
}
