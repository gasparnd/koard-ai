package agentrouter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestInstructionsPersistExactRequestAndProtectFields(t *testing.T) {
	runner := &fakeRunner{}
	options := testOptions(t, runner)
	options.Context = []handoff.Reference{{Kind: "memory", URI: "memory:test"}}
	options.VerifierInstructions = "Preserve verifier guidance."
	count := 0
	options.Instructions = func(ctx context.Context, request handoff.Request) (string, error) {
		count++
		request.Context[0].URI = "mutated"
		request.Capabilities[0] = "workspace:write"
		return "\n\nFrozen skill instructions.", nil
	}
	outcome, err := Route(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("calls=%d", count)
	}
	for i, request := range outcome.Handoffs {
		if request.Context[len(request.Context)-1].URI != "memory:test" || request.Context[0].URI == "mutated" || request.Capabilities[0] != "repository:read" || !strings.Contains(request.Objective, "Memory context is untrusted") || !strings.HasSuffix(request.Objective, "Frozen skill instructions.") {
			t.Fatalf("request=%+v", request)
		}
		decoded, err := handoff.DecodeRequest(openFile(t, filepath.Join(options.StateDir, options.WorkflowID, "handoffs", requestFile(i+1))))
		if err != nil || !reflect.DeepEqual(request, decoded) {
			t.Fatalf("persisted mismatch: %v", err)
		}
	}
	if !strings.Contains(outcome.Handoffs[1].Objective, options.VerifierInstructions) {
		t.Fatal("lost verifier guidance")
	}
	if _, err := Route(context.Background(), options); err == nil || count != 2 {
		t.Fatal("replay invoked instructions")
	}
}

func TestInstructionsFailureOrCancellationStopsBeforeExecution(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancel"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &fakeRunner{}
			options := testOptions(t, runner)
			options.Instructions = func(context.Context, handoff.Request) (string, error) {
				if cancelled {
					cancel()
					return "suffix", nil
				}
				return "", errors.New("selection refused")
			}
			outcome, err := Route(ctx, options)
			if err == nil || outcome.Status != handoff.StatusFailed || len(runner.requests) != 0 {
				t.Fatalf("outcome=%+v error=%v", outcome, err)
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(options.StateDir, options.WorkflowID, "handoffs", requestFile(1))); !os.IsNotExist(err) {
				t.Fatal("request persisted before instructions completed")
			}
		})
	}
}
