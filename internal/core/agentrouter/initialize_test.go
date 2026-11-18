package agentrouter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestInitializeRunsOnlyInsideClaimBeforeProviders(t *testing.T) {
	for _, scenario := range []string{"success", "failure", "cancelled", "unsafe"} {
		t.Run(scenario, func(t *testing.T) {
			runner := &fakeRunner{}
			options := testOptions(t, runner)
			calls := 0
			options.Initialize = func(dir string) error {
				calls++
				if len(runner.requests) != 0 {
					t.Fatal("provider ran before initialization")
				}
				if _, err := os.Stat(filepath.Join(dir, "handoffs")); err != nil {
					t.Fatal(err)
				}
				if scenario == "failure" {
					return errors.New("checkpoint unavailable")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if scenario == "unsafe" {
				options.Capabilities = []string{"repository:write"}
			}
			out, err := Route(ctx, options)
			switch scenario {
			case "success":
				if err != nil || calls != 1 || len(runner.requests) != 2 {
					t.Fatalf("%+v %v calls=%d", out, err, calls)
				}
				if _, err := Route(ctx, options); err == nil || calls != 1 {
					t.Fatal("replay initialized again")
				}
			case "failure":
				if err == nil || calls != 1 || len(runner.requests) != 0 || out.Status != handoff.StatusFailed {
					t.Fatalf("%+v %v", out, err)
				}
				stored := readOutcome(t, filepath.Join(options.StateDir, options.WorkflowID, "workflow.json"))
				if stored.Status != handoff.StatusFailed {
					t.Fatal(stored)
				}
			default:
				if err == nil || calls != 0 || len(runner.requests) != 0 {
					t.Fatalf("rejected preflight invoked initializer: %v", err)
				}
				if _, err := os.Stat(filepath.Join(options.StateDir, options.WorkflowID)); !os.IsNotExist(err) {
					t.Fatal("preflight claimed identity")
				}
			}
		})
	}
}
