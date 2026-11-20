package opencode

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
// them here so a signature change in either package fails the build instead
// of the first routed run or review attempt.
var _ agentrouter.Runner = (*Adapter)(nil)
var _ review.Reviewer = (*Adapter)(nil)

func TestAdapterProposeReturnsBoundResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: []commandResult{
		{stdout: []byte("opencode v2.2.6\n")},
		{stdout: transcript(validResultJSON("handoff-1", handoff.RoleExecutor))},
	}}
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
		t.Fatalf("invalid request invoked OpenCode %d times", len(runner.commands))
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
		t.Fatalf("unsafe request invoked OpenCode %d times", len(runner.commands))
	}
}

func TestAdapterBuildsProposalOnlyInvocation(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: []commandResult{
		{stdout: []byte("opencode v2.2.6\n")},
		{stdout: transcript(validResultJSON("handoff-1", handoff.RoleExecutor))},
	}}
	adapter := testAdapter(t, runner)

	if _, err := adapter.Propose(context.Background(), validRequest()); err != nil {
		t.Fatalf("propose valid handoff: %v", err)
	}
	command := runner.commands[1]
	joined := strings.Join(command.Args, " ")
	for _, expected := range []string{"run", "--standalone", "--archived", "--model", "openai/test-model", "--agent", "koard-proposal"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("expected invocation to contain %q: %q", expected, joined)
		}
	}
	for _, forbidden := range []string{"--auto", "--worktree", "--continue", "--session"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("proposal invocation contains forbidden flag %q: %q", forbidden, joined)
		}
	}
	if command.Dir != adapter.workspace {
		t.Fatalf("expected workspace %q, got %q", adapter.workspace, command.Dir)
	}
	policy := envValue(command.Env, "OPENCODE_CONFIG_CONTENT")
	for _, expected := range []string{
		`"action":"tool.use","resource":"*","effect":"deny"`,
		`"action":"tool.use","resource":"read:*","effect":"allow"`,
		`"action":"tool.use","resource":"glob:*","effect":"allow"`,
		`"action":"tool.use","resource":"grep:*","effect":"allow"`,
		`"action":"mcp.use","resource":"*","effect":"deny"`,
		`"action":"skill.use","resource":"*","effect":"deny"`,
		`"action":"plugin.use","resource":"*","effect":"deny"`,
		`"action":"websearch.use","resource":"*","effect":"deny"`,
		`"snapshots":false`,
	} {
		if !strings.Contains(policy, expected) {
			t.Errorf("proposal policy is missing %q: %s", expected, policy)
		}
	}
	if !strings.Contains(command.Args[len(command.Args)-1], `"id": "handoff-1"`) {
		t.Fatalf("proposal prompt does not contain the handoff request: %q", command.Args[len(command.Args)-1])
	}
}

func TestAdapterFailsClosedOnUnsupportedVersion(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: []commandResult{{stdout: []byte("opencode v2.2.2\n")}}}
	adapter := testAdapter(t, runner)

	_, err := adapter.Propose(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "2.2.3 or newer") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("unsupported version should stop before proposal, got %d commands", len(runner.commands))
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

			runner := &fakeRunner{responses: []commandResult{
				{stdout: []byte("opencode v2.2.6\n")},
				{stdout: transcript(test.output)},
			}}
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

		runner := &fakeRunner{responses: []commandResult{
			{stdout: []byte("opencode v2.2.6\n")},
			{stderr: []byte("provider unavailable\n"), err: errors.New("exit status 1")},
		}}
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

func TestAdapterReviewMaterializesSnapshotReturnsBoundResultAndCleansUp(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	var sawDir string
	var sawPrompt string
	var sawPolicy string
	var sawArgs []string
	runner := &fakeRunner{run: func(_ context.Context, cmd command) commandResult {
		if len(cmd.Args) == 1 && cmd.Args[0] == "--version" {
			return commandResult{stdout: []byte("opencode v2.2.6\n")}
		}
		sawDir = cmd.Dir
		sawArgs = cmd.Args
		sawPrompt = cmd.Args[len(cmd.Args)-1]
		sawPolicy = envValue(cmd.Env, "OPENCODE_CONFIG_CONTENT")
		content, err := os.ReadFile(filepath.Join(cmd.Dir, "change.go"))
		if err != nil {
			t.Fatalf("materialized snapshot missing change.go: %v", err)
		}
		if string(content) != "package change\n" {
			t.Fatalf("unexpected materialized content: %q", content)
		}
		return commandResult{stdout: transcript(mustMarshalReviewResult(t, review.Result{SchemaVersion: review.CurrentSchemaVersion, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved}))}
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
	for _, expected := range []string{
		`"action":"tool.use","resource":"*","effect":"deny"`,
		`"action":"tool.use","resource":"read:*","effect":"allow"`,
		`"snapshots":false`,
	} {
		if !strings.Contains(sawPolicy, expected) {
			t.Errorf("review policy is missing %q: %s", expected, sawPolicy)
		}
	}

	// Reviewer independence: every attempt is a standalone, archived,
	// non-continued run, the same guarantee TestAdapterBuildsProposalOnlyInvocation
	// pins for the executor path. Nothing here lets a provider resume a prior
	// session or otherwise carry state between review attempts.
	joined := strings.Join(sawArgs, " ")
	for _, expected := range []string{"run", "--standalone", "--archived"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("expected review invocation to contain %q: %q", expected, joined)
		}
	}
	for _, forbidden := range []string{"--auto", "--worktree", "--continue", "--session"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("review invocation contains forbidden flag %q: %q", forbidden, joined)
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
		t.Fatalf("unsafe review request invoked OpenCode %d times", len(runner.commands))
	}
}

func TestAdapterReviewFailsClosedOnUnsupportedVersion(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	runner := &fakeRunner{responses: []commandResult{{stdout: []byte("opencode v2.2.2\n")}}}
	adapter := testAdapter(t, runner)

	_, err := adapter.Review(context.Background(), request, snapshotDir)
	if err == nil || !strings.Contains(err.Error(), "2.2.3 or newer") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestAdapterReviewRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	request, snapshotDir := validReviewRequest(t)
	bound := mustMarshalReviewResult(t, review.Result{SchemaVersion: review.CurrentSchemaVersion, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved})

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

			runner := &fakeRunner{responses: []commandResult{{stdout: []byte("opencode v2.2.6\n")}, {stdout: transcript(test.output)}}}
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
	runner := &fakeRunner{responses: []commandResult{
		{stdout: []byte("opencode v2.2.6\n")},
		{stderr: []byte("provider unavailable\n"), err: errors.New("exit status 1")},
	}}
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
		Executable: "opencode",
		Workspace:  t.TempDir(),
		Model:      "openai/test-model",
		Agent:      "koard-proposal",
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

type fakeRunner struct {
	commands  []command
	responses []commandResult
	run       func(context.Context, command) commandResult
}

func (runner *fakeRunner) Run(ctx context.Context, command command) commandResult {
	runner.commands = append(runner.commands, command)
	if runner.run != nil {
		return runner.run(ctx, command)
	}
	if len(runner.responses) == 0 {
		return commandResult{err: errors.New("unexpected command")}
	}
	result := runner.responses[0]
	runner.responses = runner.responses[1:]
	return result
}

func envValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
