package opencode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
)

// The router depends on this structural contract; pin it here so a signature
// change in either package fails the build instead of the first routed run.
var _ agentrouter.Runner = (*Adapter)(nil)

func TestAdapterProposeReturnsBoundResult(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: []commandResult{
		{stdout: []byte("opencode v2.2.6\n")},
		{stdout: validResultJSON("handoff-1", handoff.RoleExecutor)},
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
	if len(runner.commands) != 0 {
		t.Fatalf("unsafe request invoked OpenCode %d times", len(runner.commands))
	}
}

func TestAdapterBuildsProposalOnlyInvocation(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: []commandResult{
		{stdout: []byte("opencode v2.2.6\n")},
		{stdout: validResultJSON("handoff-1", handoff.RoleExecutor)},
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
				{stdout: test.output},
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
