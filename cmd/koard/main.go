package main

import (
	"os"

	"github.com/koard-ai/koard/internal/cli"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = os.Stderr.WriteString("koard: cannot determine the working directory\n")
		os.Exit(1)
	}

	os.Exit(cli.Run(os.Args[1:], cwd, os.Stdout, os.Stderr))
}
