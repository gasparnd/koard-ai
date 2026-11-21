package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/koard-ai/koard/internal/skillregistry"
)

const skillHelp = `Usage:
  koard skill list [--global] [--json]
  koard skill validate [--global] [--strict] [--json]

Discover project SKILL.md files without loading or executing instructions.
--global includes supported skill directories under the user's home directory.
--strict validates Koard authoring structure, not just compatible metadata.
Project skills win duplicate names; diagnostics identify shadowed sources.
Exit codes: 0 success, 1 discovery/validation errors, 2 invalid arguments.
No registry files, settings or runtime state are written.`

func runSkill(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, skillHelp)
		return 0
	}
	if args[0] != "list" && args[0] != "validate" {
		fmt.Fprintf(stderr, "koard skill: unknown command %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("koard skill "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	global := flags.Bool("global", false, "include supported global skill directories")
	asJSON := flags.Bool("json", false, "emit structured skills and diagnostics")
	strict := false
	if args[0] == "validate" {
		flags.BoolVar(&strict, "strict", false, "enforce Koard authoring structure")
	}
	flags.Usage = func() { fmt.Fprintln(stdout, skillHelp) }
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "koard skill: unexpected positional arguments")
		return 2
	}
	home := ""
	if *global {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "koard skill: %v\n", err)
			return 1
		}
	}
	report, err := skillregistry.Scan(cwd, home, strict)
	if err != nil {
		fmt.Fprintf(stderr, "koard skill: %v\n", err)
		return 1
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "koard skill: encode report: %v\n", err)
			return 1
		}
	} else {
		for _, skill := range report.Skills {
			fmt.Fprintf(stdout, "%s %q %q\n  %q\n", skill.Scope, skill.Name, skill.Path, skill.Description)
		}
		for _, diagnostic := range report.Diagnostics {
			fmt.Fprintf(stderr, "%s [%s] %q: %q\n", diagnostic.Severity, diagnostic.Code, diagnostic.Path, diagnostic.Message)
		}
		if args[0] == "validate" && !report.HasErrors() {
			fmt.Fprintf(stdout, "Validated %d skills.\n", len(report.Skills))
		}
	}
	if report.HasErrors() {
		return 1
	}
	return 0
}
