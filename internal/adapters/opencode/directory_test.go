package opencode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
)

// Re-execute the test binary without a shell: shells can silently repair PWD
// and hide the environment mismatch that OpenCode's JS entry point observes.
func init() {
	if os.Getenv("KOARD_TEST_DIRECTORY_CHILD") != "1" {
		return
	}
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(7) }
	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	actual, err := os.Stat(cwd)
	if err != nil {
		fail(err)
	}
	declared, err := os.Stat(os.Getenv("PWD"))
	if err != nil {
		fail(err)
	}
	if !os.SameFile(actual, declared) {
		fail(fmt.Errorf("PWD does not identify process directory"))
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("opencode v2.2.6")
		os.Exit(0)
	}
	if os.Getenv("OPENCODE_CONFIG_CONTENT") != proposalPolicy {
		fail(fmt.Errorf("inspection policy changed"))
	}
	content, err := os.ReadFile(filepath.Join(os.Getenv("PWD"), "change.go"))
	if err != nil {
		fail(err)
	}
	if string(content) != os.Getenv("KOARD_TEST_DIRECTORY_CONTENT") {
		fail(fmt.Errorf("inspected wrong file bytes"))
	}
	if err := os.WriteFile(os.Getenv("KOARD_TEST_DIRECTORY_OBSERVED"), []byte(cwd), 0600); err != nil {
		fail(err)
	}
	stream, err := os.ReadFile(os.Getenv("KOARD_TEST_DIRECTORY_STREAM"))
	if err != nil {
		fail(err)
	}
	_, _ = os.Stdout.Write(stream)
	os.Exit(0)
}

func TestAdapterBindsProcessDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PWD is a Unix runtime convention")
	}
	for _, operation := range []string{"proposal", "review"} {
		for _, environment := range []string{"stale", "matching", "absent"} {
			t.Run(operation+"/"+environment, func(t *testing.T) {
				workspace, output := t.TempDir(), t.TempDir()
				live := []byte("package live\n")
				if err := os.WriteFile(filepath.Join(workspace, "change.go"), live, 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PWD", workspace)
				if environment == "stale" {
					t.Setenv("PWD", t.TempDir())
				}
				if environment == "absent" {
					if err := os.Unsetenv("PWD"); err != nil {
						t.Fatal(err)
					}
				}
				parentPWD, parentHasPWD := os.LookupEnv("PWD")
				req, snapshotDir := validReviewRequest(t)
				payload := validResultJSON("handoff-1", handoff.RoleExecutor)
				content := string(live)
				if operation == "review" {
					content = "package change\n"
					payload = mustMarshalReviewResult(t, review.Result{SchemaVersion: 1, RequestID: req.ID, Attempt: req.Attempt, Target: req.Target, Verdict: review.VerdictApproved})
				}
				stream, observed := filepath.Join(output, "stream"), filepath.Join(output, "observed")
				if err := os.WriteFile(stream, transcript(payload), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("KOARD_TEST_DIRECTORY_CHILD", "1")
				t.Setenv("KOARD_TEST_DIRECTORY_CONTENT", content)
				t.Setenv("KOARD_TEST_DIRECTORY_STREAM", stream)
				t.Setenv("KOARD_TEST_DIRECTORY_OBSERVED", observed)
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				a, err := New(Config{Executable: exe, Workspace: workspace, Model: "test/model"})
				if err != nil {
					t.Fatal(err)
				}
				if operation == "proposal" {
					got, err := a.Propose(context.Background(), validRequest())
					if err != nil || got.HandoffID != "handoff-1" {
						t.Fatalf("result=%+v error=%v", got, err)
					}
				} else {
					got, err := a.Review(context.Background(), req, snapshotDir)
					if err != nil || got.RequestID != req.ID {
						t.Fatalf("result=%+v error=%v", got, err)
					}
				}
				seen, err := os.ReadFile(observed)
				if err != nil {
					t.Fatal(err)
				}
				if operation == "review" {
					if _, err := os.Stat(string(seen)); !os.IsNotExist(err) {
						t.Fatal("materialized review directory not removed")
					}
				} else {
					want, err := os.Stat(workspace)
					if err != nil {
						t.Fatal(err)
					}
					actual, err := os.Stat(string(seen))
					if err != nil || !os.SameFile(actual, want) {
						t.Fatalf("wrong proposal directory: %q", seen)
					}
				}
				after, err := os.ReadFile(filepath.Join(workspace, "change.go"))
				if err != nil || string(after) != string(live) {
					t.Fatal("live workspace changed")
				}
				pwd, set := os.LookupEnv("PWD")
				if pwd != parentPWD || set != parentHasPWD {
					t.Fatal("parent environment changed")
				}
				if strings.TrimSpace(string(seen)) == "" {
					t.Fatal("child inspection missing")
				}
			})
		}
	}
}
