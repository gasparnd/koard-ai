package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/koard-ai/koard/internal/adapters/engram"
	"github.com/koard-ai/koard/internal/adapters/opencode"
	"github.com/koard-ai/koard/internal/application/nativeintegration"
)

const nativeHelp = `Usage: koard native setup --runtime opencode [--dry-run] [--with-engram]
Install project-local OpenCode V2 orchestration assets without overwriting files.
Preview with --dry-run. Identical files are unchanged; conflicts refuse setup.
Existing native configurations require manual adoption. No global changes,
program installation, model invocation or Engram session registration occurs.
Use a fresh host session; existing sessions retain their selected agent.
--with-engram adds the official pinned plugin and project-local MCP configuration.
Requires engram 3.7.2 on PATH; runs only its version check (also during preview).
No binary downloads or global setup. Existing differing configurations still refuse.
Host activation and memory continuity are not verified by setup.`

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
	withEngram := flags.Bool("with-engram", false, "configure project-local Engram plugin and MCP")
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
	if err == nil && *withEngram {
		err = engram.CheckNativeVersion()
		if err == nil {
			bundle, err = opencode.NativeEngramBundle()
		}
	}
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
	if *withEngram {
		if *dry {
			fmt.Fprintln(stdout, "Engram configuration preview only; no files written.")
		} else {
			fmt.Fprintln(stdout, "Engram configured locally; host activation and memory continuity are not verified.")
		}
		fmt.Fprintln(stdout, "An existing global Engram plugin may take precedence; verify plugin and MCP status in OpenCode.")
	} else {
		fmt.Fprintln(stdout, "Native assets only; host activation and Engram continuity require separate verification.")
	}
	return 0
}
