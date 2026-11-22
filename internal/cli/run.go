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

	"github.com/koard-ai/koard/internal/adapters/claude"
	"github.com/koard-ai/koard/internal/adapters/codex"
	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/application/oddworkflow"
	"github.com/koard-ai/koard/internal/application/skillselection"
	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const runUsage = "Usage: koard run --workflow <id> --task <id> --objective <text> [--policy fixed|planned] [--skills auto|off] [--global-skills]"

// Construction never invokes a provider. All required roles are resolved before
// a workflow is claimed; execution remains behind the core's read-only guard.
var workflowProvider = func(cwd string, agent config.AgentConfig) (agentrouter.Runner, error) {
	switch agent.Provider {
	case "claude":
		return claude.New(claude.Config{Workspace: cwd, Model: agent.Model})
	case "opencode":
		return opencode.New(opencode.Config{Workspace: cwd, Model: agent.Model})
	case "codex":
		return codex.New(codex.Config{Workspace: cwd, Model: agent.Model})
	default:
		return nil, fmt.Errorf("unsupported provider %q", agent.Provider)
	}
}

func runWorkflow(args []string, cwd string, stdout, stderr io.Writer) int {
	return runWorkflowMode(args, cwd, stdout, stderr, false)
}

func runWorkflowMode(args []string, cwd string, stdout, stderr io.Writer, guided bool) int {
	usage, name := runUsage, "run"
	if guided {
		usage = strings.Replace(runUsage, "koard run", "koard odd run", 1)
		name = "odd run"
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("workflow", "", "Workflow identity")
	task := flags.String("task", "", "Task identity")
	objective := flags.String("objective", "", "Requested outcome")
	policy := flags.String("policy", agentrouter.PolicyFixed, "Routing policy")
	skills := flags.String("skills", "auto", "Automatic skill selection or off")
	globalSkills := flags.Bool("global-skills", false, "Include supported global skill roots")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(stdout, usage)
		return 0
	} else if err != nil {
		fmt.Fprintf(stderr, "koard %s: %v\n%s\n", name, err, usage)
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*task) == "" || strings.TrimSpace(*objective) == "" {
		fmt.Fprintf(stderr, "koard %s: workflow, task, and objective are required; positional arguments are not supported\n", name)
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if *skills != "auto" && *skills != "off" {
		fmt.Fprintln(stderr, "koard: skills must be auto or off")
		return 2
	}
	err := executeWorkflowWithSkills(cwd, *id, *task, *objective, *policy, stdout, stderr, guided, *skills, *globalSkills)
	if err != nil {
		fmt.Fprintf(stderr, "koard %s: %v\n", name, err)
		return 1
	}
	return 0
}

func executeWorkflow(cwd, id, task, objective, policy string, stdout, stderr io.Writer) error {
	return executeWorkflowMode(cwd, id, task, objective, policy, stdout, stderr, false)
}

func executeWorkflowMode(cwd, id, task, objective, policy string, stdout, stderr io.Writer, guided bool) error {
	return executeWorkflowWithSkills(cwd, id, task, objective, policy, stdout, stderr, guided, "auto", false)
}

func executeWorkflowWithSkills(cwd, id, task, objective, policy string, stdout, stderr io.Writer, guided bool, skills string, globalSkills bool) error {
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
		if agent.Provider != "opencode" && agent.Provider != "codex" && agent.Provider != "claude" {
			return fmt.Errorf("agents.%s.provider %q is unsupported; supported providers are %q, %q and %q", step.To, agent.Provider, "opencode", "codex", "claude")
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
	var preparation oddworkflow.Preparation
	if guided {
		if policy == "" {
			policy = agentrouter.PolicyFixed
		}
		preparation, err = oddworkflow.Prepare(context.Background(), cwd, id, task, objective, policy, cfg)
		if err != nil {
			return err
		}
	}
	stateDir := filepath.Join(cwd, ".koard", "state", "runs")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options := agentrouter.Options{WorkflowID: id, TaskID: task, Objective: objective, Policy: policy, StateDir: stateDir, Workspace: cwd, Runner: runner}
	initialized := false
	if guided {
		options.Initialize = func(dir string) error { err := preparation.Initialize(cwd, dir); initialized = err == nil; return err }
	}
	if skills != "off" {
		home := ""
		if globalSkills {
			home, err = os.UserHomeDir()
			if err != nil {
				return err
			}
		}
		initialize := options.Initialize
		var selection *skillselection.Session
		options.Initialize = func(dir string) error {
			if initialize != nil {
				if err := initialize(dir); err != nil {
					return err
				}
			}
			var err error
			selection, err = skillselection.Freeze(ctx, cwd, home, dir, runner)
			return err
		}
		options.Instructions = func(ctx context.Context, request handoff.Request) (string, error) {
			return selection.Instructions(ctx, request)
		}
	}
	finish := func(runErr error) error {
		if initialized {
			return errors.Join(runErr, printODDStatus(cwd, id, stdout))
		}
		return runErr
	}
	var client memoryClient
	memoryCWD := cwd
	// A replay belongs to the router's existing refusal path, not a fresh memory
	// session. Provider validation above also remains ahead of all memory work.
	if _, inspectErr := os.Lstat(filepath.Join(stateDir, id)); os.IsNotExist(inspectErr) && cfg.Memory.Provider != "none" {
		var memoryErr error
		if cfg.Memory.Provider != "engram" {
			memoryErr = fmt.Errorf("unsupported memory provider %q", cfg.Memory.Provider)
		} else {
			memoryCWD, memoryErr = snapshot.RealWorkspace(cwd)
			if memoryErr == nil {
				client, memoryErr = memoryProvider(ctx, memoryCWD)
			}
		}
		if memoryErr == nil {
			defer closeMemory(client, stderr)
			options.Context, memoryErr = recallMemory(ctx, memoryCWD, objective, client)
			options.VerifierInstructions = memoryInstructions
		}
		if memoryErr != nil {
			fmt.Fprintln(stderr, "koard: memory unavailable:", memoryErr)
		}
	}
	outcome, err := agentrouter.Route(ctx, options)
	if outcome.WorkflowID != "" {
		fmt.Fprintf(stdout, "workflow: %s\npolicy: %s\nstatus: %s\n", id, policy, outcome.Status)
		if outcome.Candidate != nil {
			fmt.Fprintf(stdout, "candidate: %s\nsnapshot: %s\n", outcome.Candidate.ID, outcome.Candidate.SnapshotID)
		}
	}
	if err != nil {
		return finish(err)
	}
	if outcome.Status != handoff.StatusCompleted {
		return finish(fmt.Errorf("workflow %q stopped with status %q", id, outcome.Status))
	}
	if client != nil {
		retainMemory(ctx, memoryCWD, id, client, stdout, stderr)
	}
	return finish(nil)
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
	original := append([]handoff.Reference(nil), request.Context...)
	request.Context = append([]handoff.Reference(nil), request.Context...)
	for i, reference := range request.Context {
		if reference.Kind == agentrouter.ResultReferenceKind && strings.HasPrefix(reference.URI, "handoffs/") {
			if _, err := snapshot.CanonicalRelativePath("repo://" + reference.URI); err != nil {
				return handoff.Result{}, err
			}
			request.Context[i].URI = "repo://" + filepath.ToSlash(filepath.Join(runner.runPath, reference.URI))
		}
	}
	result, err := adapter.Execute(ctx, request)
	if err != nil || len(result.MemoryProposals) == 0 {
		return result, err
	}
	if err := handoff.ValidateResultFor(request, result); err != nil {
		return handoff.Result{}, err
	}
	result.MemoryProposals = append([]handoff.MemoryProposal(nil), result.MemoryProposals...)
	for i := range result.MemoryProposals {
		result.MemoryProposals[i].Evidence = append([]handoff.Reference(nil), result.MemoryProposals[i].Evidence...)
		for j, ref := range result.MemoryProposals[i].Evidence {
			for index, translated := range request.Context {
				if ref == translated {
					result.MemoryProposals[i].Evidence[j] = original[index]
					break
				}
			}
		}
	}
	return result, nil
}
