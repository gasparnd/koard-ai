package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

// The current test binary impersonates the CLI; no installed Claude is invoked.
type helperRunner struct{}

func (helperRunner) Run(ctx context.Context, c command) ([]byte, error) {
	c.name = os.Args[0]
	c.args = append([]string{"-test.run=^TestClaudeProcessHelper$", "--"}, c.args...)
	return (execRunner{}).Run(ctx, c)
}
func TestClaudeProcessHelper(t *testing.T) {
	if os.Getenv("KOARD_CLAUDE_HELPER") != "1" {
		return
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		os.Exit(9)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(10)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil || len(entries) != 0 {
		os.Exit(11)
	}
	if pwd := os.Getenv("PWD"); pwd != "" {
		real, _ := filepath.EvalSymlinks(pwd)
		canonicalCWD, _ := filepath.EvalSymlinks(cwd)
		if real != canonicalCWD {
			os.Exit(12)
		}
	}
	args := os.Args
	if contains(args, "--version") {
		fmt.Print("2.1.316 (Claude Code)")
		os.Exit(0)
	}
	if contains(args, "status") {
		fmt.Print(`{"authMethod":"claude.ai"}`)
		os.Exit(0)
	}
	if !contains(args, "--tools") || contains(args, "--bare") {
		os.Exit(13)
	}
	if !contains(args, "sonnet; DO_NOT_EXECUTE") {
		os.Exit(14)
	}
	fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"{\"schema_version\":1,\"handoff_id\":\"h1\",\"role\":\"executor\",\"status\":\"blocked\",\"summary\":\"Missing source context\"}"}`)
	os.Exit(0)
}
func TestNativeProcessBoundary(t *testing.T) {
	t.Setenv("KOARD_CLAUDE_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "test-only-do-not-forward")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-only-do-not-forward")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PWD", "/invalid-parent")
	a, _, r := fixture(t)
	a.runner = helperRunner{}
	a.model = "sonnet; DO_NOT_EXECUTE"
	got, err := a.Execute(context.Background(), r)
	if err != nil || got.Status != handoff.StatusBlocked {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEmptyContextDoesNotPretendInspection(t *testing.T) {
	a, f, r := fixture(t)
	f.answer = envelope(t, handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusBlocked, Summary: "Missing source context"})
	got, err := a.Execute(context.Background(), r)
	if err != nil || got.Status != handoff.StatusBlocked {
		t.Fatal(err)
	}
	p := strings.SplitN(f.calls[2].stdin, "\n\n", 2)
	var payload struct {
		Evidence []item `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(p[1]), &payload); err != nil || len(payload.Evidence) != 0 {
		t.Fatal("invented context", err)
	}
}
