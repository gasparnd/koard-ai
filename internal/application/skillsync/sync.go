// Package skillsync applies explicit project skill projections after validating
// the complete list. It never deletes or retargets existing links.
package skillsync

import (
	"fmt"

	"github.com/koard-ai/koard/internal/application/skillprojection"
)

type Request struct {
	Name   string
	Target skillprojection.Target
}

// Run returns the preview or successfully processed projections in declaration
// order. Preflight errors return no results and perform no writes. A later I/O
// failure keeps prior successful links; rerunning converges through Project's
// existing-target checks, not rollback or a separate ownership manifest.
func Run(workspace string, requests []Request, dryRun bool) ([]skillprojection.Result, error) {
	plan := make([]skillprojection.Result, 0, len(requests))
	for _, request := range requests {
		result, err := skillprojection.Project(workspace, request.Name, request.Target, true)
		if err != nil {
			return nil, fmt.Errorf("preflight skill %q: %w", request.Name, err)
		}
		plan = append(plan, result)
	}
	if dryRun {
		return plan, nil
	}
	completed := make([]skillprojection.Result, 0, len(requests))
	for _, request := range requests {
		result, err := skillprojection.Project(workspace, request.Name, request.Target, false)
		if err != nil {
			return completed, fmt.Errorf("stopped after %d of %d projections at skill %q: %w; previous links are kept; resolve the cause and rerun sync", len(completed), len(requests), request.Name, err)
		}
		completed = append(completed, result)
	}
	return completed, nil
}
