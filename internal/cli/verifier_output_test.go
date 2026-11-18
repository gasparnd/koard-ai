package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// S1: a failing provider process must stop both public routes before handoff 2.
func TestVerifierOutputProcessFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, guided := range []bool{false, true} {
		t.Run(map[bool]string{false: "run", true: "odd-run"}[guided], func(t *testing.T) {
			cwd := workflowProject(t)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			config := filepath.Join(cwd, ".koard/koard.yaml")
			b, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(config, []byte(strings.ReplaceAll(string(b), "provider: codex", "provider: opencode")), 0600); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.2.6'; exit 0; fi\necho '{\"sessionID\":\"s\",\"type\":\"error\"}'\necho deterministic-failure >&2\nexit 7\n"
			if err = os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			args := []string{"run", "--workflow", "demo", "--task", "task", "--objective", "Inspect existing files"}
			if guided {
				args = append([]string{"odd"}, args...)
			}
			code, out, stderr := memoryCommand(t, cwd, args...)
			if code != 1 || !strings.Contains(out, "status: failed") || !strings.Contains(stderr, "run OpenCode proposal: deterministic-failure: exit status 7") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, stderr)
			}
			if _, err = os.Stat(filepath.Join(cwd, ".koard/state/runs/demo/handoffs/2-request.json")); !os.IsNotExist(err) {
				t.Fatal("continued after failure")
			}
			t.Logf("exit=%d stdout=%q stderr=%q", code, out, stderr)
		})
	}
}
