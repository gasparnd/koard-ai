package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/koard-ai/koard/internal/adapters/claude"
	"github.com/koard-ai/koard/internal/adapters/codex"
	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	applycore "github.com/koard-ai/koard/internal/core/apply"
	"github.com/koard-ai/koard/internal/core/consent"
	reviewcore "github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/reviewmode"
)

// reviewProviders resolves the Reviewer and Corrector used by 'review run'.
// Production builds a real runtime adapter from the project's existing
// agents.verifier configuration — independent review adds no config schema of
// its own. The provider must name a supported runtime; an unknown value fails
// closed instead of silently choosing one. Tests substitute fakes by
// overriding this variable.
var reviewProviders = func(cwd string) (reviewcore.Reviewer, agentrouter.Runner, reviewcore.ReviewerIdentity, error) {
	projectConfig, err := diagnoseProject(cwd)
	if err != nil {
		return nil, nil, reviewcore.ReviewerIdentity{}, err
	}
	verifier := projectConfig.Agents.Verifier

	switch verifier.Provider {
	case "claude":
		adapter, err := claude.New(claude.Config{Workspace: cwd, Model: verifier.Model})
		if err != nil {
			return nil, nil, reviewcore.ReviewerIdentity{}, err
		}
		return adapter, adapter, reviewcore.ReviewerIdentity{Runtime: "claude", Model: verifier.Model}, nil
	case "opencode":
		adapter, err := opencode.New(opencode.Config{Workspace: cwd, Model: verifier.Model})
		if err != nil {
			return nil, nil, reviewcore.ReviewerIdentity{}, err
		}
		identity := reviewcore.ReviewerIdentity{Runtime: "opencode", Model: verifier.Model}
		return adapter, adapter, identity, nil
	case "codex":
		adapter, err := codex.New(codex.Config{Workspace: cwd, Model: verifier.Model})
		if err != nil {
			return nil, nil, reviewcore.ReviewerIdentity{}, err
		}
		identity := reviewcore.ReviewerIdentity{Runtime: "codex", Model: verifier.Model}
		return adapter, adapter, identity, nil
	default:
		return nil, nil, reviewcore.ReviewerIdentity{}, fmt.Errorf(
			"agents.verifier.provider %q is unsupported; supported providers are %q, %q and %q",
			verifier.Provider, "opencode", "codex", "claude",
		)
	}
}

func reviewStatus(args []string, cwd string, stdout io.Writer) error {
	runDir, err := reviewRunDir(args, cwd)
	if err != nil {
		return err
	}
	record, err := reviewcore.Status(runDir)
	if err != nil {
		return err
	}
	receipt, err := receiptFor(runDir, record)
	if err != nil {
		return err
	}
	applied, err := applyFor(runDir, receipt)
	if err != nil {
		return err
	}
	printReviewLifecycle(stdout, record, receipt, applied)
	return nil
}

// receiptFor looks up the verifiable receipt only once a lifecycle has
// actually settled approved; every other state never has one.
func receiptFor(runDir string, record *reviewcore.Lifecycle) (*reviewcore.Receipt, error) {
	if record == nil || record.State != reviewcore.StateApproved {
		return nil, nil
	}
	return reviewcore.ReceiptStatus(runDir)
}

// applyFor looks up the apply completion record only once a receipt exists
// to apply in the first place.
func applyFor(runDir string, receipt *reviewcore.Receipt) (*applycore.Record, error) {
	if receipt == nil {
		return nil, nil
	}
	return applycore.Status(runDir)
}

// reviewRun executes only the next authorized bounded transition for one
// workflow: the initial review when none has started, or the one permitted
// correction once a separate decision has granted it. It never loops and
// never applies files, commits, pushes, or delivers anything.
func reviewRun(args []string, cwd string, stdout io.Writer) error {
	runDir, err := reviewRunDir(args, cwd)
	if err != nil {
		return err
	}
	workflowID := filepath.Base(runDir)

	locations, err := reviewLocations(cwd)
	if err != nil {
		return err
	}
	modeCheck := func() (reviewmode.Mode, error) {
		status, err := reviewmode.Read(locations)
		if err != nil {
			return "", err
		}
		return status.Effective, nil
	}

	current, err := reviewcore.Status(runDir)
	if err != nil {
		return err
	}
	if current != nil && current.State != reviewcore.StateCorrectionRequired {
		return fmt.Errorf("review for workflow %q already settled with state %q; nothing to run", workflowID, current.State)
	}
	if current != nil && current.State == reviewcore.StateCorrectionRequired {
		decision, err := reviewcore.CorrectionDecisionStatus(runDir)
		if err != nil {
			return err
		}
		if decision == nil {
			return errors.New("a separate correction decision is required before running the next transition; use 'koard review correction grant' or 'decline'")
		}
	}

	reviewer, corrector, identity, err := reviewProviders(cwd)
	if err != nil {
		return err
	}
	options := reviewcore.Options{RunDir: runDir, Reviewer: reviewer, ReviewerIdentity: identity, ReviewMode: modeCheck, Workspace: cwd, Corrector: corrector}

	var record reviewcore.Lifecycle
	if current == nil {
		record, err = reviewcore.RunInitial(context.Background(), options)
	} else {
		record, err = reviewcore.RunCorrection(context.Background(), options)
	}
	if err != nil {
		return err
	}
	receipt, err := receiptFor(runDir, &record)
	if err != nil {
		return err
	}
	applied, err := applyFor(runDir, receipt)
	if err != nil {
		return err
	}
	printReviewLifecycle(stdout, &record, receipt, applied)
	return nil
}

// reviewApply writes the exact receipted, non-stale candidate snapshot to
// the real workspace. A valid, verified receipt is the only authority it
// requires — the same authority already granted through consent, review,
// and any correction decision. It never invokes git and never authorizes
// commit, push, pull request, merge, or release.
func reviewApply(args []string, cwd string, stdout io.Writer) error {
	runDir, err := reviewRunDir(args, cwd)
	if err != nil {
		return err
	}
	record, err := applycore.Apply(runDir, cwd)
	if err != nil {
		return err
	}
	printApplyRecord(stdout, record)
	return nil
}

func reviewCorrection(args []string, cwd string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"expected status, grant, or decline"}
	}
	action := args[0]
	if action != "status" && action != "grant" && action != "decline" {
		return usageError{fmt.Sprintf("unknown review correction action %q", action)}
	}
	runDir, err := reviewRunDir(args[1:], cwd)
	if err != nil {
		return err
	}

	if action == "status" {
		decision, err := reviewcore.CorrectionDecisionStatus(runDir)
		if err != nil {
			return err
		}
		printCorrectionDecision(stdout, decision)
		return nil
	}

	locations, err := reviewLocations(cwd)
	if err != nil {
		return err
	}
	return withReviewMutationLock(func() error {
		mode, err := reviewmode.Read(locations)
		if err != nil {
			return err
		}
		if mode.Effective == reviewmode.Off {
			return fmt.Errorf("review mode is off (decided by %s); enable it before recording a correction decision", mode.DecidedBy)
		}

		decision := consent.Granted
		if action == "decline" {
			decision = consent.Declined
		}
		record, err := reviewcore.DecideCorrection(runDir, decision)
		if err != nil {
			return err
		}
		printCorrectionDecision(stdout, &record)
		return nil
	})
}

// reviewRunDir validates --workflow and resolves it to the exact run
// directory the other review subcommands already use.
func reviewRunDir(args []string, cwd string) (string, error) {
	workflowID, err := parseWorkflowFlag(args)
	if err != nil {
		return "", err
	}
	if err := agentrouter.ValidateWorkflowID(workflowID); err != nil {
		return "", usageError{err.Error()}
	}
	if err := requireKoardProject(cwd); err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".koard", "state", "runs", workflowID), nil
}

func printReviewLifecycle(stdout io.Writer, record *reviewcore.Lifecycle, receipt *reviewcore.Receipt, applied *applycore.Record) {
	if record == nil {
		fmt.Fprintln(stdout, "Review state: not started")
		fmt.Fprintln(stdout, "  mutation and delivery: not authorized")
		return
	}
	fmt.Fprintf(stdout, "Review state: %s\n", record.State)
	fmt.Fprintf(stdout, "  workflow: %s\n", record.Target.WorkflowID)
	fmt.Fprintf(stdout, "  candidate: %s\n", record.Target.CandidateID)
	fmt.Fprintf(stdout, "  snapshot: %s\n", record.Target.SnapshotID)
	fmt.Fprintf(stdout, "  attempt: %d\n", record.Attempt)
	if record.Verdict != "" {
		fmt.Fprintf(stdout, "  verdict: %s\n", record.Verdict)
	}
	fmt.Fprintf(stdout, "  corrections used: %d/%d\n", record.CorrectionsUsed, record.MaxCorrections)
	if record.CorrectionRequestID != "" {
		fmt.Fprintf(stdout, "  correction request: %s\n", record.CorrectionRequestID)
	}
	if record.Failure != "" {
		fmt.Fprintf(stdout, "  failure: %s\n", record.Failure)
	}
	if receipt != nil {
		fmt.Fprintf(stdout, "  receipt: %s\n", receipt.ID)
	}
	if applied != nil {
		fmt.Fprintf(stdout, "  apply: %s\n", applied.State)
		fmt.Fprintln(stdout, "  delivery (commit, push, pull request, merge, release): not authorized")
	} else {
		fmt.Fprintln(stdout, "  mutation and delivery: not authorized")
	}
}

func printApplyRecord(stdout io.Writer, record applycore.Record) {
	fmt.Fprintf(stdout, "Apply: %s\n", record.State)
	fmt.Fprintf(stdout, "  workflow: %s\n", record.Target.WorkflowID)
	fmt.Fprintf(stdout, "  candidate: %s\n", record.Target.CandidateID)
	fmt.Fprintf(stdout, "  snapshot: %s\n", record.Target.SnapshotID)
	fmt.Fprintln(stdout, "  mutation: workspace files written")
	fmt.Fprintln(stdout, "  delivery (commit, push, pull request, merge, release): not authorized")
}

func printCorrectionDecision(stdout io.Writer, decision *reviewcore.CorrectionDecision) {
	if decision == nil {
		fmt.Fprintln(stdout, "Correction decision: unset")
		return
	}
	fmt.Fprintf(stdout, "Correction decision: %s\n", decision.Decision)
	fmt.Fprintf(stdout, "  workflow: %s\n", decision.Target.WorkflowID)
	fmt.Fprintf(stdout, "  correction request: %s\n", decision.CorrectionRequestID)
	fmt.Fprintln(stdout, "  authority: one proposal-only correction only")
	fmt.Fprintln(stdout, "  mutation and delivery: not authorized")
}
