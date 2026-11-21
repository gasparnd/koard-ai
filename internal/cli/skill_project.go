package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/koard-ai/koard/internal/adapters/codex"
	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/application/skillprojection"
)

const skillProjectHelp = `Usage: koard skill project --runtime opencode|codex [--dry-run] <name>
Expose one existing project skill through a relative directory link.
Options precede the name. No global sources, overwrites or configuration changes.
OpenCode: .opencode/skills/<name>; Codex: .agents/skills/<name> (shared discovery).
--dry-run checks the source and destination without writing any files.`

func runSkillProject(args []string, cwd string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("koard skill project", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	var runtime string
	seen := false
	flags.Func("runtime", "target runtime", func(value string) error {
		if seen {
			return fmt.Errorf("runtime must be specified once")
		}
		seen, runtime = true, value
		return nil
	})
	dry := flags.Bool("dry-run", false, "preview without writing")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, skillProjectHelp)
			return 0
		}
		return 2
	}
	target := skillProjectionTarget(runtime)
	if target == nil || flags.NArg() != 1 || !skillprojection.ValidName(flags.Arg(0)) {
		fmt.Fprintln(stderr, "koard skill project: require --runtime opencode|codex and one lowercase hyphenated name (at most 64 characters)")
		return 2
	}
	result, err := skillprojection.Project(cwd, flags.Arg(0), target, *dry)
	if err != nil {
		fmt.Fprintf(stderr, "koard skill project: %v\n", err)
		return 1
	}
	action := "Projected"
	if *dry {
		action = "Would project"
	}
	if result.Existing {
		action = "Already projected"
	}
	fmt.Fprintf(stdout, "%s %q -> %q\n", action, result.Destination, result.Source)
	return 0
}

func skillProjectionTarget(runtime string) skillprojection.Target {
	switch runtime {
	case "opencode":
		return opencode.SkillProjection{}
	case "codex":
		return codex.SkillProjection{}
	default:
		return nil
	}
}
