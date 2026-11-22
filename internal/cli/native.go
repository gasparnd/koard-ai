package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/application/nativeintegration"
)

const nativeHelp = `Usage: koard native setup --runtime opencode [--dry-run]
Install project-local OpenCode V2 orchestration assets without overwriting files.
Preview with --dry-run. Identical files are unchanged; conflicts refuse setup.
Existing native configurations require manual adoption. No global changes,
program installation, model invocation or Engram session registration occurs.
Use a fresh host session; existing sessions retain their selected agent.`

func runNative(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, nativeHelp)
		return 0
	}
	if args[0] != "setup" {
		fmt.Fprintln(stderr, nativeHelp)
		return 2
	}
	flags := flag.NewFlagSet("koard native setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	runtime := flags.String("runtime", "", "target runtime")
	dry := flags.Bool("dry-run", false, "preview without writing")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, nativeHelp)
			return 0
		}
		return 2
	}
	if *runtime != "opencode" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, nativeHelp)
		return 2
	}
	bundle, err := opencode.NativeBundle()
	if err != nil {
		fmt.Fprintln(stderr, "koard native:", err)
		return 1
	}
	results, err := nativeintegration.Setup(cwd, bundle, *dry)
	for _, result := range results {
		action := "Created"
		if *dry {
			action = "Would create"
		}
		if result.Existing {
			action = "Unchanged"
		}
		fmt.Fprintf(stdout, "%s %s\n", action, result.Path)
	}
	if err != nil {
		fmt.Fprintln(stderr, "koard native:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Native assets only; host activation and Engram continuity require separate verification.")
	return 0
}
