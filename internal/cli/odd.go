package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/koard-ai/koard/internal/application/oddworkflow"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/reviewmode"
)

const oddUsage = "Usage: koard odd run --workflow <id> --task <id> --objective <text> [--policy fixed|planned]\n       koard odd status --workflow <id>"

func runODD(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(stdout, oddUsage)
		return 0
	}
	if args[0] == "run" {
		return runWorkflowMode(args[1:], cwd, stdout, stderr, true)
	}
	if args[0] != "status" {
		fmt.Fprintln(stderr, oddUsage)
		return 2
	}
	id, err := parseWorkflowFlag(args[1:])
	if err == nil {
		err = agentrouter.ValidateWorkflowID(id)
	}
	if err != nil {
		fmt.Fprintln(stderr, "koard odd:", err)
		return 2
	}
	if err := requireKoardProject(cwd); err != nil {
		fmt.Fprintln(stderr, "koard odd:", err)
		return 1
	}
	if err := printODDStatus(cwd, id, stdout); err != nil {
		fmt.Fprintln(stderr, "koard odd:", err)
		return 1
	}
	return 0
}

func printODDStatus(cwd, id string, stdout io.Writer) error {
	p, err := oddworkflow.Status(context.Background(), cwd, id, func() (reviewmode.Mode, error) {
		locations, err := reviewLocations(cwd)
		if err != nil {
			return "", err
		}
		status, err := reviewmode.Read(locations)
		return status.Effective, err
	})
	if err != nil {
		return err
	}
	if p.Machine.WorkflowID != id {
		return errors.New("ODD workflow identity mismatch")
	}
	fmt.Fprintf(stdout, "ODD state: %s\n  workflow: %s\n  reason: %s\n", p.Machine.State, id, p.Reason)
	if p.Machine.ResumeState != "" {
		fmt.Fprintf(stdout, "  blocked phase: %s\n", p.Machine.ResumeState)
	}
	if p.Target != nil {
		fmt.Fprintf(stdout, "  candidate: %s\n  snapshot: %s\n", p.Target.CandidateID, p.Target.SnapshotID)
	}
	for _, action := range p.Actions {
		fmt.Fprintln(stdout, "  next (explicit action):", action)
	}
	if p.Reason == "review_disabled" {
		fmt.Fprintln(stdout, "  Review is disabled; no setting was changed. Apply requires an existing verified receipt.")
	}
	fmt.Fprintln(stdout, "  scope: guided proposal through authorized apply; delivery remains separate")
	return nil
}
