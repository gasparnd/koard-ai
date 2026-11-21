package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestAutomaticSkillsAcrossRoutes(t *testing.T) {
	for _, command := range []string{"run", "odd"} {
		for _, policy := range []string{"fixed", "planned"} {
			t.Run(command+policy, func(t *testing.T) {
				cwd := workflowProject(t)
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				body := compatibleSkill
				source := skillFile(t, cwd, "skills/testing/SKILL.md", body)
				old := workflowProvider
				t.Cleanup(func() { workflowProvider = old })
				selections, mains := 0, 0
				workflowProvider = func(_ string, cfg config.AgentConfig) (agentrouter.Runner, error) {
					return workflowFake(func(_ context.Context, req handoff.Request) (handoff.Result, error) {
						if cfg.Model != string(req.To) || len(req.Capabilities) != 1 || req.Capabilities[0] != "repository:read" {
							t.Fatalf("binding: %+v", req)
						}
						result := handoff.Result{SchemaVersion: 1, HandoffID: req.ID, Role: req.To, Status: handoff.StatusCompleted, Summary: "Complete"}
						if strings.HasSuffix(req.ID, "-skills") {
							selections++
							if !strings.Contains(req.Objective, "testing") || strings.Contains(req.Objective, "Run the project's tests.") {
								t.Fatal("selector must see metadata, not instructions")
							}
							result.Artifacts = []handoff.Reference{{Kind: "skill", URI: "koard-skill:s001", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))}}
							if err := os.WriteFile(source, []byte("changed after capture"), 0600); err != nil {
								t.Fatal(err)
							}
						} else {
							mains++
							if !strings.Contains(req.Objective, body) {
								t.Fatal("exact frozen instructions not delivered")
							}
						}
						return result, nil
					}), nil
				}
				args := []string{"run", "--workflow", "auto", "--task", "task", "--objective", "Verify tests", "--policy", policy}
				if command == "odd" {
					args = append([]string{"odd"}, args...)
				}
				var out, errOut bytes.Buffer
				if code := Run(args, cwd, &out, &errOut); code != 0 {
					t.Fatalf("code=%d out=%s err=%s", code, &out, &errOut)
				}
				want := 2
				if policy == "planned" {
					want = 3
				}
				if selections != want || mains != want {
					t.Fatalf("calls=%d/%d", selections, mains)
				}
				dir := filepath.Join(cwd, ".koard/state/runs/auto")
				data, err := os.ReadFile(filepath.Join(dir, "workflow.json"))
				if err != nil {
					t.Fatal(err)
				}
				var outcome agentrouter.Outcome
				if err := json.Unmarshal(data, &outcome); err != nil {
					t.Fatal(err)
				}
				for _, req := range outcome.Handoffs {
					delivery, err := os.ReadFile(filepath.Join(dir, "skills", string(req.To), "delivery.json"))
					if err != nil {
						t.Fatal(err)
					}
					digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(req.Objective)))
					if !strings.Contains(string(delivery), digest) {
						t.Fatal("delivery does not bind objective")
					}
				}
				if code := Run(args, cwd, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 || selections != want {
					t.Fatal("replay selected again")
				}
			})
		}
	}
}

func TestAutomaticSkillsControlsAndFailures(t *testing.T) {
	for _, scenario := range []string{"off", "empty selection", "unknown", "digest", "duplicate", "blocked", "next", "global excluded", "global included", "invalid mode"} {
		t.Run(scenario, func(t *testing.T) {
			cwd := workflowProject(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			root, path := cwd, "skills/testing/SKILL.md"
			if strings.HasPrefix(scenario, "global") {
				root, path = home, ".agents/skills/testing/SKILL.md"
			}
			skillFile(t, root, path, compatibleSkill)
			old := workflowProvider
			t.Cleanup(func() { workflowProvider = old })
			selections, mains := 0, 0
			workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
				return workflowFake(func(_ context.Context, req handoff.Request) (handoff.Result, error) {
					r := handoff.Result{SchemaVersion: 1, HandoffID: req.ID, Role: req.To, Status: handoff.StatusCompleted, Summary: "Complete"}
					if !strings.HasSuffix(req.ID, "-skills") {
						mains++
						return r, nil
					}
					selections++
					ref := handoff.Reference{Kind: "skill", URI: "koard-skill:s001", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(compatibleSkill)))}
					switch scenario {
					case "unknown":
						ref.URI = "repo://outside"
					case "digest":
						ref.Digest = "sha256:wrong"
					case "blocked":
						r.Status = handoff.StatusBlocked
					case "next":
						r.Next = &handoff.Transition{Role: handoff.RoleVerifier, Reason: "extra"}
					}
					if scenario != "empty selection" {
						r.Artifacts = []handoff.Reference{ref}
					}
					if scenario == "duplicate" {
						r.Artifacts = append(r.Artifacts, ref)
					}
					return r, nil
				}), nil
			}
			args := []string{"run", "--workflow", "auto", "--task", "t", "--objective", "Goal"}
			if scenario == "off" {
				args = append(args, "--skills=off")
			}
			if scenario == "invalid mode" {
				args = append(args, "--skills=maybe")
			}
			if scenario == "global included" {
				args = append(args, "--global-skills")
			}
			var out, errOut bytes.Buffer
			code := Run(args, cwd, &out, &errOut)
			switch scenario {
			case "off", "global excluded":
				if code != 0 || selections != 0 || mains != 2 {
					t.Fatalf("code=%d calls=%d/%d %s", code, selections, mains, &errOut)
				}
			case "empty selection", "global included":
				if code != 0 || selections != 2 || mains != 2 {
					t.Fatalf("code=%d calls=%d/%d %s", code, selections, mains, &errOut)
				}
			case "invalid mode":
				if code != 2 || mains != 0 || selections != 0 {
					t.Fatalf("code=%d", code)
				}
			default:
				if code != 1 || selections != 1 || mains != 0 {
					t.Fatalf("code=%d calls=%d/%d %s", code, selections, mains, &errOut)
				}
			}
		})
	}
}
