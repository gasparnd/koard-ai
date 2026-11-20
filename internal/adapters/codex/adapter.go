// Package codex translates provider-neutral handoffs and review requests into
// bounded, read-only Codex CLI runs.
//
// The adapter is deliberately narrow. It owns one process shape, one prompt
// contract, and one strict result shape; it never routes roles, persists
// workflow state, mutates the workspace, or authorizes delivery. The core
// depends on the contracts this package satisfies structurally, so the
// inward-only dependency direction survives a second runtime.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/mutation"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

const (
	// minimumMajor/minor/patch is the oldest Codex CLI verified to expose the
	// whole guard surface this adapter depends on: `exec`, `-s read-only`,
	// `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, and
	// `--output-last-message`. Anything older would need a different, unproven
	// invocation, so the adapter refuses it instead of guessing.
	minimumMajor = 0
	minimumMinor = 149
	minimumPatch = 0
)

var versionPattern = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)`)

// Config identifies the Codex executable and the bounded read-only runtime.
// Agent is intentionally absent: Codex profiles are user configuration, and
// the adapter ignores user configuration on purpose.
type Config struct {
	Executable string
	Workspace  string
	Model      string
}

// Adapter translates a provider-neutral handoff or review request into one
// read-only Codex invocation and validates the returned result.
type Adapter struct {
	executable string
	workspace  string
	model      string
	runner     commandRunner
}

// New creates an adapter backed by the local Codex executable.
func New(config Config) (*Adapter, error) {
	return newAdapter(config, execRunner{})
}

func newAdapter(config Config, runner commandRunner) (*Adapter, error) {
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = "codex"
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
		runner:     runner,
	}, nil
}

// Execute runs one handoff through the read-only path. It exists so the
// adapter satisfies the router's runner contract without the core importing a
// provider package.
func (adapter *Adapter) Execute(ctx context.Context, request handoff.Request) (handoff.Result, error) {
	return adapter.Propose(ctx, request)
}

// Propose performs a read-only Codex run and returns exactly one result bound
// to the supplied request. The OS-level sandbox, not a per-tool policy, is the
// guard: writes fail inside Codex instead of being offered as a decision.
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

	output, err := adapter.run(ctx, adapter.workspace, proposalPrompt(string(encoded)), "run Codex proposal")
	if err != nil {
		return handoff.Result{}, err
	}

	decoded, err := handoff.DecodeResult(bytes.NewReader(output))
	if err != nil {
		return handoff.Result{}, fmt.Errorf("decode Codex proposal result: %w", err)
	}
	if err := handoff.ValidateResultFor(request, decoded); err != nil {
		return handoff.Result{}, fmt.Errorf("validate Codex proposal result: %w", err)
	}

	return decoded, nil
}

// Review performs a read-only Codex run over the exact snapshot being
// reviewed — materialized into a disposable directory, never the live
// workspace, which may already have drifted from what was consented to — and
// returns exactly one strictly decoded result. Binding that result to the
// request (schema version, request id, attempt, target, verdict) is
// review.validateResultFor's job in the core, not the adapter's: the adapter
// never certifies its own identity or authority, and the review core rejects
// any result whose binding does not match before it becomes lifecycle state.
func (adapter *Adapter) Review(ctx context.Context, request review.Request, snapshotDir string) (review.Result, error) {
	if err := validateReviewRequest(request); err != nil {
		return review.Result{}, err
	}
	if err := adapter.checkVersion(ctx); err != nil {
		return review.Result{}, err
	}

	materialized, err := os.MkdirTemp("", "koard-review-*")
	if err != nil {
		return review.Result{}, fmt.Errorf("stage review materialization: %w", err)
	}
	defer os.RemoveAll(materialized)
	if err := snapshot.Materialize(snapshotDir, materialized, request.Snapshot); err != nil {
		return review.Result{}, fmt.Errorf("materialize reviewed snapshot: %w", err)
	}

	encoded, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return review.Result{}, fmt.Errorf("marshal review request: %w", err)
	}

	output, err := adapter.run(ctx, materialized, reviewPrompt(string(encoded)), "run Codex review")
	if err != nil {
		return review.Result{}, err
	}

	decoded, err := decodeReviewResult(output)
	if err != nil {
		return review.Result{}, fmt.Errorf("decode Codex review result: %w", err)
	}
	return decoded, nil
}

// run invokes Codex once inside dir and returns the exact final message. The
// last message travels through a file because Codex prints progress to stdout
// alongside the answer; reading a stream would mean guessing which line is the
// result.
func (adapter *Adapter) run(ctx context.Context, dir, prompt, action string) ([]byte, error) {
	staging, err := os.MkdirTemp("", "koard-codex-*")
	if err != nil {
		return nil, fmt.Errorf("stage Codex output: %w", err)
	}
	defer os.RemoveAll(staging)
	outputPath := filepath.Join(staging, "last-message.json")

	arguments := append([]string{"exec"}, policyFlags()...)
	arguments = append(arguments,
		"-C", dir,
		"-m", adapter.model,
		"--output-last-message", outputPath,
		prompt,
	)

	result := adapter.runner.Run(ctx, command{
		Name: adapter.executable,
		Args: arguments,
		Dir:  dir,
	})
	if result.err != nil {
		return nil, commandError(action, result)
	}

	message, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read Codex final message: %w", err)
	}
	trimmed := bytes.TrimSpace(message)
	if len(trimmed) == 0 {
		return nil, errors.New("Codex returned no final message")
	}
	return trimmed, nil
}

// policyFlags is the complete read-only surface every invocation shares. The
// user's Codex configuration is ignored so a user-level sandbox, hook, MCP
// server, or project-doc setting cannot widen the guard, and rule files are
// ignored for the same reason. Session persistence is disabled so a later run
// can never resume this one.
func policyFlags() []string {
	return []string{
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"-s", "read-only",
		"--color", "never",
	}
}

func validateProposalRequest(request handoff.Request) error {
	if err := request.Validate(); err != nil {
		return fmt.Errorf("invalid proposal request: %w", err)
	}
	if request.ExpectedOutput.Format != "application/json" {
		return fmt.Errorf("proposal expected_output.format must be %q", "application/json")
	}
	if err := mutation.RequireReadOnly(request.Capabilities); err != nil {
		return fmt.Errorf("proposal request rejected: %w", err)
	}
	return nil
}

func validateReviewRequest(request review.Request) error {
	if err := mutation.RequireReadOnly(request.Capabilities); err != nil {
		return fmt.Errorf("review request rejected: %w", err)
	}
	return nil
}

func decodeReviewResult(encoded []byte) (review.Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var result review.Result
	if err := decoder.Decode(&result); err != nil {
		return review.Result{}, err
	}
	if _, err := decoder.Token(); err != nil && !errors.Is(err, io.EOF) {
		return review.Result{}, fmt.Errorf("decode trailing data: %w", err)
	} else if err == nil {
		return review.Result{}, errors.New("multiple JSON values are not supported")
	}
	return result, nil
}

func proposalPrompt(request string) string {
	return "Perform only repository inspection needed to answer this handoff. " +
		"Do not modify files or perform delivery operations. Return exactly one JSON object " +
		"with no markdown fence or commentary. Required fields: schema_version (1), handoff_id (copy request.id), " +
		"role (copy request.to), status (completed, blocked, or failed), summary (nonblank string). " +
		"Optional artifacts and evidence are arrays of objects with kind, uri, and optional digest. " +
		"Use existing repo://relative/path files for repository artifacts; never invent file bytes or digests. " +
		"Optional next contains role and reason: role is planner, executor, or verifier; reason is nonblank. " +
		"It is guidance, not authority. Optional memory_proposals is only for a completed verifier, at most 8 objects " +
		"with type, title, content, rationale, evidence, sensitivity, and optional uncertainty. " +
		"Type is decision, architecture, bugfix, pattern, config, discovery, or preference. " +
		"Title, content, and rationale are nonblank and at most 256, 4096, and 1024 bytes respectively. " +
		"Sensitivity is nonsensitive, sensitive, or unknown. Evidence contains 1 to 8 references copied exactly " +
		"from request.context (kind, uri, digest), with nonblank kind at most 128 bytes, uri at most 2048 bytes, " +
		"and lowercase sha256: plus 64 hexadecimal digits. Uncertainty contains at most 8 nonblank strings of at most 512 bytes. " +
		"Memory proposals do not authorize persistence. Omit optional fields when unnecessary. " +
		"No additional fields are allowed at any level. Do not fetch the schema URI; the result fields are defined here. " +
		"If inspection is unavailable, return blocked with an honest summary.\n\n" +
		"Handoff request:\n" + request
}

func reviewPrompt(request string) string {
	return "Independently review the exact snapshot materialized at the current working directory. " +
		"Do not modify files or perform delivery operations. Cite findings only against the artifacts " +
		"and digests named in the review request. Return exactly one JSON object that satisfies the " +
		"following result contract, with no markdown fence or commentary. Required fields: schema_version (1), " +
		"request_id (copy request.id), attempt (copy request.attempt), target (copy the entire request.target object unchanged), " +
		"verdict (approved, correction_required, or rejected). Optional findings is an array of objects with " +
		"id, severity (warning or blocking), summary, and optional evidence. IDs and summaries must be nonblank. " +
		"Finding evidence contains objects with only uri and digest matching the supplied snapshot entries exactly. " +
		"correction_required requires at least one finding; approved must not contain blocking findings. " +
		"No additional fields are allowed at any level: do not add reviewer identity or a top-level summary. " +
		"Choose the verdict from observed evidence; never invent approval, findings, or digests.\n\n" +
		"Review request:\n" + request
}

func (adapter *Adapter) checkVersion(ctx context.Context) error {
	result := adapter.runner.Run(ctx, command{
		Name: adapter.executable,
		Args: []string{"--version"},
		Dir:  adapter.workspace,
	})
	if result.err != nil {
		return commandError("check Codex version", result)
	}
	major, minor, patch, err := parseVersion(string(result.stdout))
	if err != nil {
		return err
	}
	if versionBefore(major, minor, patch, minimumMajor, minimumMinor, minimumPatch) {
		return fmt.Errorf("Codex %d.%d.%d is unsupported; this adapter requires %d.%d.%d or newer", major, minor, patch, minimumMajor, minimumMinor, minimumPatch)
	}
	return nil
}

func parseVersion(output string) (int, int, int, error) {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return 0, 0, 0, fmt.Errorf("parse Codex version from %q", strings.TrimSpace(output))
	}
	parts := make([]int, 3)
	for index := range parts {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse Codex version: %w", err)
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

// commandError prefixes one failed process with the action that failed and
// keeps the provider's stderr, so the operator reads the real cause instead of
// a bare exit status.
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()

	return commandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
}
