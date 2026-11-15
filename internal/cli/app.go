package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const Version = "0.1.0-dev"

var projectDirectories = []string{
	".koard/agents",
	".koard/skills",
	".koard/state",
	".koard/workflows",
}

const defaultConfig = `schema_version: 1
project:
  name: koard-project

workflow:
  default: odd

memory:
  provider: engram

agents:
  planner:
    provider: unset
    model: unset
  executor:
    provider: unset
    model: unset
  verifier:
    provider: unset
    model: unset
`

func Run(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}

	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return 0
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, Version)
		return 0
	case "init":
		if err := initializeProject(cwd); err != nil {
			fmt.Fprintf(stderr, "koard init: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Koard project initialized.")
		return 0
	case "doctor":
		if err := diagnoseProject(cwd); err != nil {
			fmt.Fprintf(stderr, "koard doctor: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Koard project configuration is present.")
		return 0
	default:
		fmt.Fprintf(stderr, "koard: unknown command %q\n", args[0])
		fmt.Fprintln(stderr, "Run 'koard help' to see available commands.")
		return 2
	}
}

func initializeProject(cwd string) error {
	for _, directory := range projectDirectories {
		if err := os.MkdirAll(filepath.Join(cwd, directory), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}

	configPath := filepath.Join(cwd, ".koard", "koard.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect configuration: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}

	return nil
}

func diagnoseProject(cwd string) error {
	configPath := filepath.Join(cwd, ".koard", "koard.yaml")
	info, err := os.Stat(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("configuration not found; run 'koard init'")
		}
		return fmt.Errorf("inspect configuration: %w", err)
	}
	if info.IsDir() {
		return errors.New(".koard/koard.yaml is a directory, expected a file")
	}

	return nil
}

func printHelp(output io.Writer) {
	fmt.Fprintln(output, `Koard orchestrates portable AI development workflows.

Usage:
  koard <command>

Commands:
  init       Create the project-level Koard structure
  doctor     Check the local Koard configuration
  version    Print the CLI version
  help       Show this help`)
}
