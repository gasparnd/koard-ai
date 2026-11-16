package codex

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
)

// The read-only guarantee is only as strong as the exact argv the adapter
// builds. These tests pin the surface positionally, so a future change cannot
// widen it by adding a flag that a substring check would never notice.

// TestExactArgvForPromptRun pins every prompt-run argument against the spec
// itself rather than against policyFlags(), which would move with the change.
func TestExactArgvForPromptRun(t *testing.T) {
	t.Parallel()

	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON("handoff-1", handoff.RoleExecutor))
	adapter := testAdapter(t, runner)
	if _, err := adapter.Propose(context.Background(), validRequest()); err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}

	args := runner.commands[1].Args
	out := flagValue(t, args, "--output-last-message")
	expected := []string{
		"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--skip-git-repo-check", "-s", "read-only", "--color", "never",
		"-C", adapter.workspace, "-m", "openai/test-model",
		"--output-last-message", out,
	}
	if len(args) != len(expected)+1 {
		t.Fatalf("argc=%d want %d: %q", len(args), len(expected)+1, args)
	}
	for index, want := range expected {
		if args[index] != want {
			t.Fatalf("arg[%d]=%q want %q", index, args[index], want)
		}
	}
	for index, arg := range args {
		if arg == "-s" && (index+1 >= len(args) || args[index+1] != "read-only") {
			t.Fatalf("-s followed by %q", args[index+1])
		}
		switch arg {
		case "--dangerously-bypass-approvals-and-sandbox", "--approve-for-me", "--add-dir",
			"--image", "-i", "resume", "fork", "--full-auto", "--yolo":
			t.Fatalf("forbidden surface %q in %q", arg, args)
		}
		if strings.Contains(arg, "workspace-write") || strings.Contains(arg, "danger-full-access") {
			t.Fatalf("forbidden sandbox value %q", arg)
		}
	}
	if !strings.HasPrefix(args[len(args)-1], "Perform only repository inspection") {
		t.Fatalf("last argument is not the prompt: %q", args[len(args)-1])
	}
}

// TestModelCannotAddArguments proves the one configuration value that reaches
// argv cannot smuggle a second flag: it stays a single -m value.
func TestModelCannotAddArguments(t *testing.T) {
	t.Parallel()

	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON("handoff-1", handoff.RoleExecutor))
	adapter, err := newAdapter(Config{
		Executable: "codex",
		Workspace:  t.TempDir(),
		Model:      "gpt --dangerously-bypass-approvals-and-sandbox -s danger-full-access",
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Propose(context.Background(), validRequest()); err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}

	args := runner.commands[1].Args
	if len(args) != 16 {
		t.Fatalf("argument count changed: %d %q", len(args), args)
	}
	for index, arg := range args {
		if arg == "-m" && args[index+1] != adapter.model {
			t.Fatalf("model was rewritten: %q", args[index+1])
		}
	}
}

// TestVersionGateBoundaries covers the gate edges: an accepted version must
// reach the prompt, and every refused one must stop after the version probe.
func TestVersionGateBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		version string
		ok      bool
	}{
		{version: "codex-cli 0.149.0", ok: true},
		{version: "codex-cli 0.149.1", ok: true},
		{version: "codex-cli 0.150.0", ok: true},
		{version: "codex-cli 1.0.0", ok: true},
		{version: "0.149.0-alpha.3", ok: true},
		{version: "codex-cli 0.148.9"},
		{version: "codex-cli 0.149"},
		{version: "codex-cli"},
		{version: ""},
	}
	for _, test := range cases {
		version := test.version
		runner := &fakeRunner{run: func(_ context.Context, cmd command) commandResult {
			if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
				return commandResult{stdout: []byte(version + "\n")}
			}
			return commandResult{err: errors.New("prompt run must not happen")}
		}}
		adapter := testAdapter(t, runner)

		_, err := adapter.Propose(context.Background(), validRequest())
		if test.ok {
			if len(runner.commands) != 2 {
				t.Fatalf("%q: never reached the prompt (%d commands)", version, len(runner.commands))
			}
			continue
		}
		if err == nil || len(runner.commands) != 1 {
			t.Fatalf("%q: refusal missing (err=%v, %d commands)", version, err, len(runner.commands))
		}
	}
}

// TestReviewIsolationAndCleanupOnFailure proves the disposable directory is
// removed on the failure path too, not only when a review succeeds.
func TestReviewIsolationAndCleanupOnFailure(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	var dir string
	runner := &fakeRunner{run: func(_ context.Context, cmd command) commandResult {
		if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
			return commandResult{stdout: []byte("codex-cli 0.149.0\n")}
		}
		dir = cmd.Dir
		if _, err := os.Stat(dir + "/change.go"); err != nil {
			t.Fatalf("snapshot not materialized: %v", err)
		}
		return commandResult{stderr: []byte("boom\n"), err: errors.New("exit status 1")}
	}}
	adapter := testAdapter(t, runner)

	if _, err := adapter.Review(context.Background(), request, snapshotDir); err == nil {
		t.Fatal("expected the provider failure to surface")
	}
	if dir == "" || dir == adapter.workspace || dir == snapshotDir {
		t.Fatalf("review ran in %q", dir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("materialized directory survived the failure: %v", err)
	}
}

// TestReviewRejectsStdoutOnlyResult is the property the output file exists for:
// a valid-looking result printed to stdout must never be accepted.
func TestReviewRejectsStdoutOnlyResult(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	runner := &fakeRunner{run: func(_ context.Context, cmd command) commandResult {
		if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
			return commandResult{stdout: []byte("codex-cli 0.149.0\n")}
		}
		return commandResult{stdout: mustMarshalReviewResult(t, review.Result{
			SchemaVersion: review.CurrentSchemaVersion,
			RequestID:     request.ID,
			Attempt:       request.Attempt,
			Target:        request.Target,
			Verdict:       review.VerdictApproved,
		})}
	}}
	adapter := testAdapter(t, runner)

	if _, err := adapter.Review(context.Background(), request, snapshotDir); err == nil {
		t.Fatal("accepted a result that existed only on stdout")
	}
}
