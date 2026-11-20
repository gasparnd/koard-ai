package skillselection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

type fakeRunner func(context.Context, handoff.Request) (handoff.Result, error)

func (f fakeRunner) Execute(ctx context.Context, r handoff.Request) (handoff.Result, error) {
	return f(ctx, r)
}

func TestSelectionStopsOnCancellationAndAuditFailure(t *testing.T) {
	for _, mode := range []string{"cancel before", "cancel during", "request audit", "delivery audit", "provider", "too many", "evidence", "binding", "prompt limit"} {
		t.Run(mode, func(t *testing.T) {
			project, run := t.TempDir(), t.TempDir()
			path := filepath.Join(project, "skills/test")
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: test\ndescription: Tests\n---\nInstructions"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			runner := fakeRunner(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
				calls++
				result := handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Selection"}
				switch mode {
				case "cancel during":
					cancel()
				case "provider":
					return result, errors.New("provider failed")
				case "delivery audit":
					if err := os.Mkdir(filepath.Join(run, "skills/executor/delivery.json"), 0700); err != nil {
						t.Fatal(err)
					}
				case "too many":
					for i := 0; i < 6; i++ {
						result.Artifacts = append(result.Artifacts, handoff.Reference{Kind: "skill", URI: "koard-skill:s001"})
					}
				case "evidence":
					result.Evidence = []handoff.Reference{{Kind: "source", URI: "repo://file"}}
				case "binding":
					result.HandoffID = "unrelated"
				}
				return result, nil
			})
			s, err := Freeze(ctx, project, "", run, runner)
			if err != nil {
				t.Fatal(err)
			}
			req := handoff.Request{SchemaVersion: 1, ID: "run-1", TaskID: "task", From: handoff.RolePlanner, To: handoff.RoleExecutor, Objective: "Goal", Capabilities: []string{"repository:read"}, ExpectedOutput: handoff.OutputContract{Format: "application/json", Schema: "koard://schemas/handoff-result/v1"}}
			if mode == "cancel before" {
				cancel()
			}
			if mode == "request audit" {
				if err := os.Mkdir(filepath.Join(run, "skills/executor"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "prompt limit" {
				req.Objective = strings.Repeat("x", MaxSelectionBytes)
			}
			suffix, err := s.Instructions(ctx, req)
			if err == nil || suffix != "" {
				t.Fatalf("accepted %s: %q %v", mode, suffix, err)
			}
			if strings.HasPrefix(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			want := 1
			if mode == "cancel before" || mode == "request audit" || mode == "prompt limit" {
				want = 0
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
		})
	}
}
