package engram

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CheckNativeVersion checks only the CLI, never setup, a server, or a memory store.
// The pinned host plugin and MCP must resolve the same executable at host launch.
func CheckNativeVersion() error {
	if override := os.Getenv("ENGRAM_BIN"); strings.TrimSpace(override) != "" && override != "engram" {
		return fmt.Errorf("native Engram requires ENGRAM_BIN unset or exactly engram; no configuration was written")
	}
	binary, err := exec.LookPath("engram")
	if err != nil {
		return fmt.Errorf("native Engram requires engram 3.7.2 on PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.WaitDelay = time.Second
	var output nativeVersionOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("check native Engram version: %w", err)
	}
	if output.overflow || strings.TrimSpace(output.text.String()) != "engram 3.7.2" {
		return fmt.Errorf("native Engram requires version 3.7.2 for the bundled plugin; other versions are not verified")
	}
	return nil
}

type nativeVersionOutput struct {
	text     strings.Builder
	overflow bool
}

func (b *nativeVersionOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 128 - b.text.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.text.Write(p)
	return n, nil
}
