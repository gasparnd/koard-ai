package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/reviewmode"
)

func reviewConsent(args []string, cwd string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"expected status, grant, or decline"}
	}
	action := args[0]
	if action != "status" && action != "grant" && action != "decline" {
		return usageError{fmt.Sprintf("unknown review consent action %q", action)}
	}
	workflowID, err := parseWorkflowFlag(args[1:])
	if err != nil {
		return err
	}
	if err := agentrouter.ValidateWorkflowID(workflowID); err != nil {
		return usageError{err.Error()}
	}
	if err := requireKoardProject(cwd); err != nil {
		return err
	}
	runDir := filepath.Join(cwd, ".koard", "state", "runs", workflowID)

	if action == "status" {
		target, record, err := consent.Status(runDir)
		if err != nil {
			return err
		}
		printConsentStatus(stdout, target, record)
		return nil
	}

	locations, err := reviewLocations(cwd)
	if err != nil {
		return err
	}
	mode, err := reviewmode.Read(locations)
	if err != nil {
		return err
	}
	if mode.Effective == reviewmode.Off {
		return fmt.Errorf("review mode is off (decided by %s); enable it before recording consent", mode.DecidedBy)
	}

	decision := consent.Granted
	if action == "decline" {
		decision = consent.Declined
	}
	record, err := consent.Decide(runDir, decision)
	if err != nil {
		return err
	}
	printConsentStatus(stdout, record.Target, &record)
	return nil
}

func parseWorkflowFlag(args []string) (string, error) {
	if len(args) == 0 {
		return "", usageError{"--workflow is required"}
	}
	var workflowID string
	seen := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		var value string
		switch {
		case argument == "--workflow":
			if index+1 == len(args) {
				return "", usageError{"--workflow needs a value"}
			}
			index++
			value = args[index]
		case strings.HasPrefix(argument, "--workflow="):
			value = strings.TrimPrefix(argument, "--workflow=")
		default:
			return "", usageError{fmt.Sprintf("unexpected argument %q", argument)}
		}
		if seen {
			return "", usageError{"--workflow may be specified only once"}
		}
		seen = true
		workflowID = value
	}
	if workflowID == "" {
		return "", usageError{"--workflow needs a value"}
	}
	return workflowID, nil
}

func printConsentStatus(output io.Writer, target consent.Target, record *consent.Record) {
	decision := "unset"
	authority := "none"
	if record != nil {
		decision = string(record.Decision)
		if record.Decision == consent.Granted {
			authority = "independent review of this snapshot only"
		}
	}
	fmt.Fprintf(output, "Review consent: %s\n", decision)
	fmt.Fprintf(output, "  workflow: %s\n", target.WorkflowID)
	fmt.Fprintf(output, "  candidate: %s\n", target.CandidateID)
	fmt.Fprintf(output, "  snapshot: %s\n", target.SnapshotID)
	fmt.Fprintf(output, "  risk: %s\n", target.RiskTier)
	fmt.Fprintf(output, "  authority: %s\n", authority)
	fmt.Fprintln(output, "  mutation and delivery: not authorized")
}
