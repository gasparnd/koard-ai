package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/koard-ai/koard/internal/application/skillprojection"
	"github.com/koard-ai/koard/internal/application/skillsync"
)

const syncHelp = `Usage: koard sync [--dry-run]
Synchronize only the skill projections declared in .koard/koard.yaml sync.skills.
Each entry requires name and runtimes (opencode and/or codex).
--dry-run validates the complete list and previews it without writing files.
No implicit skills, deletion, overwrite, runtime settings or program updates.
Exit codes: 0 success, 1 configuration/projection error, 2 invalid arguments.`

func runSync(args []string, cwd string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("koard sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	dry := flags.Bool("dry-run", false, "preview without writing")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, syncHelp)
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "koard sync: unexpected positional arguments")
		return 2
	}
	config, err := diagnoseProject(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "koard sync: %v\n", err)
		return 1
	}
	requests := make([]skillsync.Request, 0)
	for _, skill := range config.Sync.Skills {
		if !skillprojection.ValidName(skill.Name) {
			fmt.Fprintf(stderr, "koard sync: invalid portable skill name %q; use lowercase hyphenated names of at most 64 characters\n", skill.Name)
			return 1
		}
		for _, runtime := range skill.Runtimes {
			target := skillProjectionTarget(runtime)
			if target == nil {
				fmt.Fprintf(stderr, "koard sync: unsupported runtime %q for skill %q; use opencode or codex\n", runtime, skill.Name)
				return 1
			}
			requests = append(requests, skillsync.Request{Name: skill.Name, Target: target})
		}
	}
	if len(requests) == 0 {
		fmt.Fprintln(stdout, "No skills configured for sync.")
		return 0
	}
	results, err := skillsync.Run(cwd, requests, *dry)
	created, unchanged := 0, 0
	for _, result := range results {
		action := "Projected"
		if *dry {
			action = "Would project"
		}
		if result.Existing {
			action = "Already projected"
			unchanged++
		} else {
			created++
		}
		fmt.Fprintf(stdout, "%s %q -> %q\n", action, result.Destination, result.Source)
	}
	if err != nil {
		fmt.Fprintf(stderr, "koard sync: %v\n", err)
		return 1
	}
	if *dry {
		fmt.Fprintf(stdout, "Sync preview: %d to create, %d unchanged.\n", created, unchanged)
	} else {
		fmt.Fprintf(stdout, "Sync complete: %d created, %d unchanged.\n", created, unchanged)
	}
	return 0
}
