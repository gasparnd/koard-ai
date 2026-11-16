package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/mutation"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/risk"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

// The router and the review core depend on these structural contracts; pin
// them here so a signature change in either package fails the build instead of
// the first routed run or review attempt.
var _ agentrouter.Runner = (*Adapter)(nil)
var _ review.Reviewer = (*Adapter)(nil)

func TestAdapterProposeReturnsBoundResult(t *testing.T) {
	t.Parallel()

	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON("handoff-1", handoff.RoleExecutor))
	adapter := testAdapter(t, runner)

	result, err := adapter.Propose(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}
	if result.HandoffID != "handoff-1" || result.Role != handoff.RoleExecutor {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("expected version and proposal commands, got %d", len(runner.commands))
	}
}

func TestAdapterRejectsInvalidRequestBeforeInvocation(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	adapter := testAdapter(t, runner)
	request := validRequest()
	request.ID = ""

	_, err := adapter.Propose(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected request validation error, got %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("invalid request invoked Codex %d times", len(runner.commands))
	}
}

func TestAdapterRejectsNonProposalCapabilities(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	adapter := testAdapter(t, runner)
	request := validRequest()
	request.Capabilities = append(request.Capabilities, "repository:write")

	_, err := adapter.Propose(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "repository:write") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
	// The adapter must fail through the shared boundary, not its own check, so
	// the two callers cannot drift apart.
	var blocked *mutation.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected the shared mutation block, got %T", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("unsafe request invoked Codex %d times", len(runner.commands))
	}
}

func TestAdapterBuildsReadOnlyInvocation(t *testing.T) {
	t.Parallel()

	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON("handoff-1", handoff.RoleExecutor))
	adapter := testAdapter(t, runner)

	if _, err := adapter.Propose(context.Background(), validRequest()); err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}
	command := runner.commands[1]
	joined := strings.Join(command.Args, " ")
	for _, expected := range []string{
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"-s read-only",
		"--color never",
		"-C " + adapter.workspace,
		"-m openai/test-model",
		"--output-last-message",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("expected invocation to contain %q: %q", expected, joined)
		}
	}
	for _, forbidden := range []string{
		"--dangerously-bypass",
		"--approve-for-me",
		"--add-dir",
		"workspace-write",
		"danger-full-access",
		"--image",
		"resume",
		"fork",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("invocation contains forbidden surface %q: %q", forbidden, joined)
		}
	}
	if command.Dir != adapter.workspace {
		t.Fatalf("expected workspace %q, got %q", adapter.workspace, command.Dir)
	}
	outputPath := flagValue(t, command.Args, "--output-last-message")
	if filepath.Dir(outputPath) == adapter.workspace {
		t.Fatalf("Codex output must be staged outside the reviewed workspace: %q", outputPath)
	}
	if !strings.Contains(command.Args[len(command.Args)-1], `"id": "handoff-1"`) {
		t.Fatalf("proposal prompt does not contain the handoff request: %q", command.Args[len(command.Args)-1])
	}
}

func TestAdapterIgnoresStdoutAndReadsFinalMessageFileOnly(t *testing.T) {
	t.Parallel()

	// Codex interleaves progress with the answer on stdout. A result that only
	// exists there must never be accepted, so the fake writes a non-JSON stdout
	// and a valid final message file.
	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON("handoff-1", handoff.RoleExecutor))
	adapter := testAdapter(t, runner)

	if _, err := adapter.Propose(context.Background(), validRequest()); err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}
}

func TestAdapterFailsClosedWhenFinalMessageIsAbsentOrEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message []byte
		write   bool
		expect  string
	}{
		{name: "missing file", write: false, expect: "read Codex final message"},
		{name: "empty file", message: []byte("  \n"), write: true, expect: "no final message"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := promptRunnerWith(t, "codex-cli 0.149.0", test.message, test.write)
			adapter := testAdapter(t, runner)

			_, err := adapter.Propose(context.Background(), validRequest())
			if err == nil || !strings.Contains(err.Error(), test.expect) {
				t.Fatalf("expected error containing %q, got %v", test.expect, err)
			}
		})
	}
}

func TestAdapterFailsClosedOnUnsupportedVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		expect  string
	}{
		{name: "older version", version: "codex-cli 0.148.9", expect: "0.149.0 or newer"},
		{name: "unparsable version", version: "unknown", expect: "parse Codex version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{version: test.version}
			adapter := testAdapter(t, runner)

			_, err := adapter.Propose(context.Background(), validRequest())
			if err == nil || !strings.Contains(err.Error(), test.expect) {
				t.Fatalf("expected error containing %q, got %v", test.expect, err)
			}
			if len(runner.commands) != 1 {
				t.Fatalf("unsupported version should stop before the prompt, got %d commands", len(runner.commands))
			}
		})
	}
}

func TestAdapterRejectsInvalidOrUnboundOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   []byte
		expected string
	}{
		{name: "markdown fence", output: []byte("```json\n{}\n```"), expected: "decode"},
		{name: "unknown field", output: []byte(`{"schema_version":1,"handoff_id":"handoff-1","role":"executor","status":"completed","summary":"ok","authority":true}`), expected: "unknown field"},
		{name: "trailing value", output: append(validResultJSON("handoff-1", handoff.RoleExecutor), []byte(" {}")...), expected: "multiple JSON values"},
		{name: "wrong identity", output: validResultJSON("handoff-other", handoff.RoleExecutor), expected: "does not match request"},
		{name: "wrong role", output: validResultJSON("handoff-1", handoff.RoleVerifier), expected: "does not match request destination"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := promptRunner(t, "codex-cli 0.149.0", test.output)
			adapter := testAdapter(t, runner)

			_, err := adapter.Propose(context.Background(), validRequest())
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestAdapterPreservesRunnerFailureAndCancellation(t *testing.T) {
	t.Parallel()

	t.Run("stderr", func(t *testing.T) {
		t.Parallel()

		runner := promptRunner(t, "codex-cli 0.149.0", nil)
		runner.fail = &commandResult{stderr: []byte("provider unavailable\n"), err: errors.New("exit status 1")}
		adapter := testAdapter(t, runner)

		_, err := adapter.Propose(context.Background(), validRequest())
		if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
			t.Fatalf("expected useful stderr, got %v", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &fakeRunner{run: func(ctx context.Context, _ command) commandResult {
			return commandResult{err: ctx.Err()}
		}}
		adapter := testAdapter(t, runner)

		_, err := adapter.Propose(ctx, validRequest())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	})
}

func TestNewValidatesConfiguration(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	file := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		config Config
		expect string
	}{
		{name: "missing workspace", config: Config{Model: "m"}, expect: "workspace is required"},
		{name: "missing directory", config: Config{Workspace: filepath.Join(workspace, "missing"), Model: "m"}, expect: "inspect workspace"},
		{name: "workspace is a file", config: Config{Workspace: file, Model: "m"}, expect: "is not a directory"},
		{name: "missing model", config: Config{Workspace: workspace}, expect: "model is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := New(test.config)
			if err == nil || !strings.Contains(err.Error(), test.expect) {
				t.Fatalf("expected error containing %q, got %v", test.expect, err)
			}
		})
	}
}

func TestNewRequiresCommandRunner(t *testing.T) {
	t.Parallel()

	_, err := newAdapter(Config{Workspace: t.TempDir(), Model: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "command runner is required") {
		t.Fatalf("expected runner validation error, got %v", err)
	}
}

func TestAdapterReviewMaterializesSnapshotReturnsBoundResultAndCleansUp(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	var sawDir string
	var sawArgs []string
	var sawPrompt string
	runner := &fakeRunner{run: func(_ context.Context, cmd command) commandResult {
		if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
			return commandResult{stdout: []byte("codex-cli 0.149.0\n")}
		}
		sawDir = cmd.Dir
		sawArgs = cmd.Args
		sawPrompt = cmd.Args[len(cmd.Args)-1]
		content, err := os.ReadFile(filepath.Join(cmd.Dir, "change.go"))
		if err != nil {
			t.Fatalf("materialized snapshot missing change.go: %v", err)
		}
		if string(content) != "package change\n" {
			t.Fatalf("unexpected materialized content: %q", content)
		}
		outputPath := flagValue(t, cmd.Args, "--output-last-message")
		if err := os.WriteFile(outputPath, mustMarshalReviewResult(t, review.Result{
			SchemaVersion: review.CurrentSchemaVersion,
			RequestID:     request.ID,
			Attempt:       request.Attempt,
			Target:        request.Target,
			Verdict:       review.VerdictApproved,
		}), 0o644); err != nil {
			t.Fatal(err)
		}
		return commandResult{stdout: []byte("progress only\n")}
	}}
	adapter := testAdapter(t, runner)

	result, err := adapter.Review(context.Background(), request, snapshotDir)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if result.Verdict != review.VerdictApproved || result.RequestID != request.ID {
		t.Fatalf("unexpected result: %#v", result)
	}
	if sawDir == "" || sawDir == adapter.workspace {
		t.Fatalf("review must run against a materialized snapshot directory, not the live workspace: %q", sawDir)
	}
	if _, err := os.Stat(sawDir); !os.IsNotExist(err) {
		t.Fatalf("materialized directory %q should be cleaned up after review, stat err=%v", sawDir, err)
	}
	if !strings.Contains(sawPrompt, `"id": "review-1"`) {
		t.Fatalf("review prompt does not contain the review request: %q", sawPrompt)
	}
	joined := strings.Join(sawArgs, " ")
	for _, expected := range []string{"exec", "-s read-only", "--ephemeral", "--ignore-user-config", "-C " + sawDir} {
		if !strings.Contains(joined, expected) {
			t.Errorf("expected review invocation to contain %q: %q", expected, joined)
		}
	}
	// Reviewer independence: every attempt is a fresh, non-resumed process.
	for _, forbidden := range []string{"resume", "fork", "--add-dir", "workspace-write", "danger-full-access"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("review invocation contains forbidden surface %q: %q", forbidden, joined)
		}
	}
}

func TestAdapterReviewRejectsNonReadOnlyCapabilities(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	request.Capabilities = append(request.Capabilities, "repository:write")
	runner := &fakeRunner{}
	adapter := testAdapter(t, runner)

	_, err := adapter.Review(context.Background(), request, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "repository:write") {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
	var blocked *mutation.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected the shared mutation block, got %T", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("unsafe review request invoked Codex %d times", len(runner.commands))
	}
}

func TestAdapterReviewFailsClosedOnUnsupportedVersion(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	runner := &fakeRunner{version: "codex-cli 0.148.9"}
	adapter := testAdapter(t, runner)

	_, err := adapter.Review(context.Background(), request, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "0.149.0 or newer") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestAdapterReviewRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	bound := mustMarshalReviewResult(t, review.Result{
		SchemaVersion: review.CurrentSchemaVersion,
		RequestID:     request.ID,
		Attempt:       request.Attempt,
		Target:        request.Target,
		Verdict:       review.VerdictApproved,
	})

	tests := []struct {
		name     string
		output   []byte
		expected string
	}{
		{name: "markdown fence", output: []byte("```json\n{}\n```"), expected: "decode"},
		{name: "unknown field", output: []byte(`{"schema_version":1,"request_id":"review-1","attempt":1,"target":{},"verdict":"approved","extra":true}`), expected: "unknown field"},
		{name: "trailing value", output: append(append([]byte{}, bound...), []byte(" {}")...), expected: "multiple JSON values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := promptRunner(t, "codex-cli 0.149.0", test.output)
			adapter := testAdapter(t, runner)

			_, err := adapter.Review(context.Background(), request, snapshotDir)
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestAdapterReviewPreservesRunnerFailure(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	runner := promptRunner(t, "codex-cli 0.149.0", nil)
	runner.fail = &commandResult{stderr: []byte("provider unavailable\n"), err: errors.New("exit status 1")}
	adapter := testAdapter(t, runner)

	_, err := adapter.Review(context.Background(), request, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected useful stderr, got %v", err)
	}
}

func validReviewRequest(t *testing.T) (review.Request, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "change.go"), []byte("package change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshotDir := t.TempDir()
	candidateID := "sha256:" + strings.Repeat("a", 64)
	captured, err := snapshot.Capture(context.Background(), workspace, snapshotDir, candidateID, []handoff.Reference{{Kind: "source", URI: "repo://change.go"}})
	if err != nil {
		t.Fatal(err)
	}
	target := consent.Target{WorkflowID: "workflow-1", CandidateID: candidateID, SnapshotID: captured.ID, RiskTier: risk.Passive}
	request := review.Request{
		SchemaVersion: review.CurrentSchemaVersion,
		ID:            "review-1",
		Attempt:       1,
		Target:        target,
		Reviewer:      review.ReviewerIdentity{Runtime: "test", Model: "reviewer-v1"},
		Capabilities:  []string{"repository:read"},
		Snapshot:      captured.Manifest,
	}
	return request, snapshotDir
}

func mustMarshalReviewResult(t *testing.T, result review.Result) []byte {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func testAdapter(t *testing.T, runner commandRunner) *Adapter {
	t.Helper()

	adapter, err := newAdapter(Config{
		Executable: "codex",
		Workspace:  t.TempDir(),
		Model:      "openai/test-model",
	}, runner)
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	return adapter
}

func validRequest() handoff.Request {
	return handoff.Request{
		SchemaVersion: handoff.CurrentSchemaVersion,
		ID:            "handoff-1",
		TaskID:        "task-1",
		From:          handoff.RolePlanner,
		To:            handoff.RoleExecutor,
		Objective:     "Propose the requested change without modifying the workspace.",
		Capabilities:  []string{"repository:read"},
		ExpectedOutput: handoff.OutputContract{
			Format: "application/json",
			Schema: "koard://schemas/handoff-result/v1",
		},
	}
}

func validResultJSON(id string, role handoff.Role) []byte {
	return []byte(`{"schema_version":1,"handoff_id":"` + id + `","role":"` + string(role) + `","status":"completed","summary":"Proposal completed."}`)
}

// promptRunner answers the version probe and, for a prompt run, writes the
// supplied message to the exact --output-last-message path the adapter chose —
// the same path a real Codex process would fill.
func promptRunner(t *testing.T, version string, message []byte) *fakeRunner {
	t.Helper()
	return promptRunnerWith(t, version, message, message != nil)
}

func promptRunnerWith(t *testing.T, version string, message []byte, write bool) *fakeRunner {
	t.Helper()

	runner := &fakeRunner{version: version}
	runner.run = func(_ context.Context, cmd command) commandResult {
		if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
			if runner.versionErr != nil {
				return commandResult{err: runner.versionErr}
			}
			return commandResult{stdout: []byte(runner.version + "\n")}
		}
		if runner.fail != nil {
			return *runner.fail
		}
		if write {
			outputPath := flagValue(t, cmd.Args, "--output-last-message")
			if err := os.WriteFile(outputPath, message, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return commandResult{stdout: []byte("codex progress\n")}
	}
	return runner
}

type fakeRunner struct {
	commands   []command
	version    string
	versionErr error
	fail       *commandResult
	run        func(context.Context, command) commandResult
}

func (runner *fakeRunner) Run(ctx context.Context, command command) commandResult {
	runner.commands = append(runner.commands, command)
	if runner.run != nil {
		return runner.run(ctx, command)
	}
	if len(command.Args) == 1 && command.Args[0] == "--version" {
		if runner.versionErr != nil {
			return commandResult{err: runner.versionErr}
		}
		return commandResult{stdout: []byte(runner.version + "\n")}
	}
	if runner.fail != nil {
		return *runner.fail
	}
	return commandResult{err: errors.New("unexpected command")}
}

func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for index, arg := range args {
		if arg == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	t.Fatalf("flag %q not found in %v", flag, args)
	return ""
}
