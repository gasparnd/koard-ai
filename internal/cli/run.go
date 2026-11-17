package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/koard-ai/koard/internal/adapters/codex"
	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
)

const runUsage = "Usage: koard run --workflow <id> --task <id> --objective <text> [--policy fixed|planned]"

// Construction never invokes a provider. All required roles are resolved before
// a workflow is claimed; execution remains behind the core's read-only guard.
var workflowProvider = func(cwd string, agent config.AgentConfig) (agentrouter.Runner, error) {
	switch agent.Provider {
	case "opencode":
		return opencode.New(opencode.Config{Workspace: cwd, Model: agent.Model})
	case "codex":
		return codex.New(codex.Config{Workspace: cwd, Model: agent.Model})
	default:
		return nil, fmt.Errorf("unsupported provider %q", agent.Provider)
	}
}

func runWorkflow(args []string, cwd string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("workflow", "", "Workflow identity")
	task := flags.String("task", "", "Task identity")
	objective := flags.String("objective", "", "Requested outcome")
	policy := flags.String("policy", agentrouter.PolicyFixed, "Routing policy")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(stdout, runUsage)
		return 0
	} else if err != nil {
		fmt.Fprintf(stderr, "koard run: %v\n%s\n", err, runUsage)
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*task) == "" || strings.TrimSpace(*objective) == "" {
		fmt.Fprintln(stderr, "koard run: workflow, task, and objective are required; positional arguments are not supported")
		fmt.Fprintln(stderr, runUsage)
		return 2
	}
	err := executeWorkflow(cwd, *id, *task, *objective, *policy, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "koard run: %v\n", err)
		return 1
	}
	return 0
}

func executeWorkflow(cwd, id, task, objective, policy string, stdout io.Writer) error {
	if err := agentrouter.ValidateWorkflowID(id); err != nil {
		return err
	}
	steps, err := agentrouter.PolicySteps(policy)
	if err != nil {
		return err
	}
	cfg, err := diagnoseProject(cwd)
	if err != nil {
		return err
	}
	agents := map[handoff.Role]config.AgentConfig{handoff.RolePlanner: cfg.Agents.Planner, handoff.RoleExecutor: cfg.Agents.Executor, handoff.RoleVerifier: cfg.Agents.Verifier}
	runner := workflowRunner{roles: make(map[handoff.Role]agentrouter.Runner), runPath: filepath.Join(".koard", "state", "runs", id)}
	for _, step := range steps {
		agent := agents[step.To]
		if agent.Provider != "opencode" && agent.Provider != "codex" {
			return fmt.Errorf("agents.%s.provider %q is unsupported; supported providers are %q and %q", step.To, agent.Provider, "opencode", "codex")
		}
		if strings.TrimSpace(agent.Model) == "" || agent.Model == "unset" {
			return fmt.Errorf("agents.%s.model must select a model before execution", step.To)
		}
		adapter, err := workflowProvider(cwd, agent)
		if err != nil {
			return fmt.Errorf("configure %s: %w", step.To, err)
		}
		runner.roles[step.To] = adapter
	}
	stateDir := filepath.Join(cwd, ".koard", "state", "runs")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	outcome, err := agentrouter.Route(ctx, agentrouter.Options{WorkflowID: id, TaskID: task, Objective: objective, Policy: policy, StateDir: stateDir, Workspace: cwd, Runner: runner})
	if outcome.WorkflowID != "" {
		fmt.Fprintf(stdout, "workflow: %s\npolicy: %s\nstatus: %s\n", id, policy, outcome.Status)
		if outcome.Candidate != nil {
			fmt.Fprintf(stdout, "candidate: %s\nsnapshot: %s\n", outcome.Candidate.ID, outcome.Candidate.SnapshotID)
		}
	}
	if err != nil {
		return err
	}
	if outcome.Status != handoff.StatusCompleted {
		return fmt.Errorf("workflow %q stopped with status %q", id, outcome.Status)
	}
	return nil
}

type workflowRunner struct {
	roles   map[handoff.Role]agentrouter.Runner
	runPath string
}

func (runner workflowRunner) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	adapter := runner.roles[request.To]
	if adapter == nil {
		return handoff.Result{}, fmt.Errorf("no provider for role %q", request.To)
	}
	// Core evidence is run-relative. Resolve only its result references for the
	// adapter's workspace view, retaining the exact persisted bytes and digest.
	request.Context = append([]handoff.Reference(nil), request.Context...)
	for i, reference := range request.Context {
		if i == 0 && reference.Kind == agentrouter.ResultReferenceKind && strings.HasPrefix(reference.URI, "handoffs/") {
			request.Context[i].URI = "repo://" + filepath.ToSlash(filepath.Join(runner.runPath, reference.URI))
		}
	}
	return adapter.Execute(ctx, request)
}
