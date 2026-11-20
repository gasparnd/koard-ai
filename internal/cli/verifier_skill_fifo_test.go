//go:build darwin || linux

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestVerifierSkillFIFO(t *testing.T) {
	if os.Getenv("KOARD_FIFO_CHILD") == "1" {
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(99)
		}
		os.Exit(Run([]string{"skill", "list", "--json"}, cwd, os.Stdout, os.Stderr))
	}
	for _, tc := range []struct{ name, relative, code string }{
		{"root", "skills", "root"},
		{"file", "skills/example/SKILL.md", "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// os.Getwd in the child may return the canonical spelling on macOS.
			dir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tc.relative)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("TMPDIR", t.TempDir())
			t.Setenv("KOARD_FIFO_CHILD", "1")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVerifierSkillFIFO$")
			cmd.Dir = dir
			cmd.Env = append(cmd.Environ(), "PWD="+dir)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("discovery blocked on FIFO: %s", out)
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 {
				t.Fatalf("want exit 1; got %v: %s", err, out)
			}
			var report struct {
				Skills      []json.RawMessage                       `json:"skills"`
				Diagnostics []struct{ Severity, Code, Path string } `json:"diagnostics"`
			}
			if err := json.Unmarshal(out, &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Skills) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("unexpected report: %s", out)
			}
			d := report.Diagnostics[0]
			if d.Severity != "error" || d.Code != tc.code || d.Path != path {
				t.Fatalf("unexpected diagnostic: %+v", d)
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("FIFO mutated: %v", err)
			}
		})
	}
}
