package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/koard-ai/koard/internal/core/handoff"
)

const (
	minimumMajor = 2
	minimumMinor = 2
	minimumPatch = 3

	// proposalPolicy denies every capability and lifts only repository
	// inspection. Managed and Console policy outrank inline configuration, so an
	// administrator allow can widen it; the adapter still passes no
	// auto-approval flag, which keeps any non-denied permission prompt from
	// being granted in this non-interactive run.
	proposalPolicy = `{"snapshots":false,"policies":[{"action":"tool.use","resource":"*","effect":"deny"},{"action":"tool.use","resource":"read:*","effect":"allow"},{"action":"tool.use","resource":"glob:*","effect":"allow"},{"action":"tool.use","resource":"grep:*","effect":"allow"},{"action":"mcp.use","resource":"*","effect":"deny"},{"action":"skill.use","resource":"*","effect":"deny"},{"action":"plugin.use","resource":"*","effect":"deny"},{"action":"websearch.use","resource":"*","effect":"deny"}]}`
)

var versionPattern = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)`)

// Config identifies the OpenCode executable and the bounded proposal runtime.
type Config struct {
	Executable string
	Workspace  string
	Model      string
	Agent      string
}

// Adapter translates a provider-neutral handoff into one proposal-only
// OpenCode invocation and validates the returned handoff result.
type Adapter struct {
	executable string
	workspace  string
	model      string
	agent      string
	runner     commandRunner
}

// New creates an adapter backed by the local OpenCode executable.
func New(config Config) (*Adapter, error) {
	return newAdapter(config, execRunner{})
}

func newAdapter(config Config, runner commandRunner) (*Adapter, error) {
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = "opencode"
	}
	workspace := strings.TrimSpace(config.Workspace)
	if workspace == "" {
		return nil, errors.New("workspace is required")
	}
	absoluteWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(absoluteWorkspace)
	if err != nil {
		return nil, fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace %q is not a directory", absoluteWorkspace)
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("model is required")
	}
	if runner == nil {
		return nil, errors.New("command runner is required")
	}

	return &Adapter{
		executable: executable,
		workspace:  absoluteWorkspace,
		model:      model,
		agent:      strings.TrimSpace(config.Agent),
		runner:     runner,
	}, nil
}

// Execute runs one handoff through the proposal-only path. It exists so the
// adapter satisfies the router's runner contract without the core importing a
// provider package.
func (adapter *Adapter) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	return adapter.Propose(ctx, request)
}

// Propose performs a read-only OpenCode run and returns exactly one result
// bound to the supplied request. It never enables OpenCode auto-approval,
// worktrees, or session continuation.
func (adapter *Adapter) Propose(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	if err := validateProposalRequest(request); err != nil {
		return handoff.Result{}, err
	}
	if err := adapter.checkVersion(ctx); err != nil {
		return handoff.Result{}, err
	}

	encoded, err := handoff.MarshalRequest(request)
	if err != nil {
		return handoff.Result{}, fmt.Errorf("marshal handoff request: %w", err)
	}
	prompt := proposalPrompt(string(encoded))
	arguments := []string{
		"run",
		"--standalone",
		"--archived",
		"--model", adapter.model,
	}
	if adapter.agent != "" {
		arguments = append(arguments, "--agent", adapter.agent)
	}
	arguments = append(arguments, prompt)

	result := adapter.runner.Run(ctx, command{
		Name: adapter.executable,
		Args: arguments,
		Dir:  adapter.workspace,
		Env:  []string{"OPENCODE_CONFIG_CONTENT=" + proposalPolicy},
	})
	if result.err != nil {
		return handoff.Result{}, commandError("run OpenCode proposal", result)
	}

	decoded, err := handoff.DecodeResult(bytes.NewReader(result.stdout))
	if err != nil {
		return handoff.Result{}, fmt.Errorf("decode OpenCode proposal result: %w", err)
	}
	if err := handoff.ValidateResultFor(request, decoded); err != nil {
		return handoff.Result{}, fmt.Errorf("validate OpenCode proposal result: %w", err)
	}

	return decoded, nil
}

func validateProposalRequest(request handoff.Request) error {
	if err := request.Validate(); err != nil {
		return fmt.Errorf("invalid proposal request: %w", err)
	}
	if request.ExpectedOutput.Format != "application/json" {
		return fmt.Errorf("proposal expected_output.format must be %q", "application/json")
	}
	for _, capability := range request.Capabilities {
		if capability != "repository:read" {
			return fmt.Errorf("capability %q is not allowed for proposal-only execution", capability)
		}
	}
	return nil
}

func (adapter *Adapter) checkVersion(ctx context.Context) error {
	result := adapter.runner.Run(ctx, command{
		Name: adapter.executable,
		Args: []string{"--version"},
		Dir:  adapter.workspace,
	})
	if result.err != nil {
		return commandError("check OpenCode version", result)
	}
	major, minor, patch, err := parseVersion(string(result.stdout))
	if err != nil {
		return err
	}
	if versionBefore(major, minor, patch, minimumMajor, minimumMinor, minimumPatch) {
		return fmt.Errorf("OpenCode %d.%d.%d is unsupported; proposal policies require 2.2.3 or newer", major, minor, patch)
	}
	return nil
}

func parseVersion(output string) (int, int, int, error) {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return 0, 0, 0, fmt.Errorf("parse OpenCode version from %q", strings.TrimSpace(output))
	}
	parts := make([]int, 3)
	for index := range parts {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse OpenCode version: %w", err)
		}
		parts[index] = value
	}
	return parts[0], parts[1], parts[2], nil
}

func versionBefore(major, minor, patch, requiredMajor, requiredMinor, requiredPatch int) bool {
	if major != requiredMajor {
		return major < requiredMajor
	}
	if minor != requiredMinor {
		return minor < requiredMinor
	}
	return patch < requiredPatch
}

func proposalPrompt(request string) string {
	return "Perform only repository inspection needed to answer this handoff. " +
		"Do not modify files or perform delivery operations. Return exactly one JSON object " +
		"that satisfies the requested handoff result schema, with no markdown fence or commentary.\n\n" +
		"Handoff request:\n" + request
}

func commandError(action string, result commandResult) error {
	message := strings.TrimSpace(string(result.stderr))
	if message == "" {
		return fmt.Errorf("%s: %w", action, result.err)
	}
	return fmt.Errorf("%s: %s: %w", action, message, result.err)
}

type command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type commandResult struct {
	stdout []byte
	stderr []byte
	err    error
}

type commandRunner interface {
	Run(context.Context, command) commandResult
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, specification command) commandResult {
	process := exec.CommandContext(ctx, specification.Name, specification.Args...)
	process.Dir = specification.Dir
	process.Env = mergeEnvironment(os.Environ(), specification.Env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()

	return commandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
}

func mergeEnvironment(base, overrides []string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	set := func(entry string) {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			return
		}
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, entry := range base {
		set(entry)
	}
	for _, entry := range overrides {
		set(entry)
	}

	merged := make([]string, 0, len(order))
	for _, name := range order {
		merged = append(merged, name+"="+values[name])
	}
	return merged
}
