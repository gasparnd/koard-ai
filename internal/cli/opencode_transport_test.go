package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the public CLI through real subprocess I/O without invoking a model.
func TestOpenCodeEventTransportCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, guided := range []bool{false, true} {
		for _, valid := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{true: "odd", false: "run"}[guided], map[bool]string{true: "valid", false: "error"}[valid]}, "/"), func(t *testing.T) {
				cwd := workflowProject(t)
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				path := filepath.Join(cwd, ".koard", "koard.yaml")
				config, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, []byte(strings.ReplaceAll(string(config), "provider: codex", "provider: opencode")), 0600); err != nil {
					t.Fatal(err)
				}
				bin := t.TempDir()
				for i, role := range []string{"executor", "verifier"} {
					result := map[string]any{"schema_version": 1, "handoff_id": []string{"demo-1", "demo-2"}[i], "role": role, "status": "completed", "summary": "Read-only inspection"}
					payload, _ := json.Marshal(result)
					var stream []byte
					for j, kind := range []string{"step_start", "text", "step_finish"} {
						part := map[string]any{"type": strings.ReplaceAll(kind, "_", "-"), "messageID": "message", "sessionID": "session"}
						if kind == "text" {
							part["text"] = string(payload)
						}
						if kind == "step_finish" {
							part["reason"] = "stop"
						}
						b, _ := json.Marshal(map[string]any{"type": kind, "timestamp": j + 1, "sessionID": "session", "part": part})
						stream = append(stream, append(b, '\n')...)
					}
					if !valid {
						stream = append(stream, []byte("{\"type\":\"error\",\"sessionID\":\"session\"}\n")...)
					}
					if err = os.WriteFile(filepath.Join(bin, role), stream, 0600); err != nil {
						t.Fatal(err)
					}
				}
				script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.2.6'; exit 0; fi\n[ \"$1 $2 $3 $4 $5 $6\" = 'run --standalone --archived --format json --model' ] || exit 9\ncat '" + bin + "/'\"$7\"\n"
				if err = os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				args := []string{"run", "--workflow", "demo", "--task", "task", "--objective", "Inspect existing files"}
				if guided {
					args = append([]string{"odd"}, args...)
				}
				code, out, stderr := memoryCommand(t, cwd, args...)
				if valid {
					if code != 0 || stderr != "" || !strings.Contains(out, "status: completed") {
						t.Fatalf("%d %s %s", code, out, stderr)
					}
				} else {
					if code != 1 || !strings.Contains(stderr, "run OpenCode proposal transport: OpenCode reported a run error") || !strings.Contains(out, "status: failed") {
						t.Fatalf("%d %s %s", code, out, stderr)
					}
					if _, err := os.Stat(filepath.Join(cwd, ".koard/state/runs/demo/handoffs/2-request.json")); !os.IsNotExist(err) {
						t.Fatal("continued after provider error")
					}
				}
			})
		}
	}
}
