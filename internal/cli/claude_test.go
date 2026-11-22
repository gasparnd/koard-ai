package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/adapters/claude"
	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestClaudeProviderConstruction(t *testing.T) {
	root := writeProviderProject(t, "claude", "sonnet")
	r, c, id, err := reviewProviders(root)
	if err != nil || id.Runtime != "claude" || id.Model != "sonnet" || any(r) != any(c) {
		t.Fatalf("%+v %v", id, err)
	}
	if _, ok := r.(*claude.Adapter); !ok {
		t.Fatalf("%T", r)
	}
	w, err := workflowProvider(root, config.AgentConfig{Provider: "claude", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.(*claude.Adapter); !ok {
		t.Fatalf("%T", w)
	}
}

func TestClaudeWorkflowComposition(t *testing.T) {
	for _, cmd := range []string{"run", "odd"} {
		for _, policy := range []string{"fixed", "planned"} {
			t.Run(cmd+"/"+policy, func(t *testing.T) {
				root := workflowProject(t)
				p := filepath.Join(root, ".koard/koard.yaml")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				b = []byte(strings.ReplaceAll(string(b), "provider: codex", "provider: claude"))
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
				old := workflowProvider
				t.Cleanup(func() { workflowProvider = old })
				calls := 0
				workflowProvider = func(_ string, cfg config.AgentConfig) (agentrouter.Runner, error) {
					return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
						calls++
						if cfg.Model != string(r.To) {
							t.Fatal("wrong role")
						}
						if r.To == handoff.RoleVerifier && cfg.Provider != "claude" {
							t.Fatal("wrong provider")
						}
						return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Supplied material evaluated"}, nil
					}), nil
				}
				args := []string{cmd}
				if cmd == "odd" {
					args = append(args, "run")
				}
				args = append(args, "--workflow", "claude-test", "--task", "task", "--objective", "Explain supplied evidence", "--policy", policy, "--skills=off")
				var out, diagnostic bytes.Buffer
				code := Run(args, root, &out, &diagnostic)
				want := 2
				if policy == "planned" {
					want = 3
				}
				if code != 0 || diagnostic.Len() != 0 || calls != want {
					t.Fatalf("%d %s calls=%d", code, &diagnostic, calls)
				}
				for _, path := range []string{"consent.json", "review", "apply.json"} {
					if _, err := os.Stat(filepath.Join(root, ".koard/state/runs/claude-test", path)); !os.IsNotExist(err) {
						t.Fatal("unauthorized side effect")
					}
				}
			})
		}
	}
}
