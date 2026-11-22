package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeSetup(t *testing.T) {
	for _, scenario := range []string{"preview", "install", "conflict", "alternate config", "symlink parent", "symlink destination", "late conflict"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			var out, errOut bytes.Buffer
			args := []string{"native", "setup", "--runtime", "opencode"}
			write := func(path, content string) {
				t.Helper()
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "preview":
				args = append(args, "--dry-run")
			case "conflict":
				write(".opencode/agents/koard.md", "human content")
			case "alternate config":
				write("opencode.jsonc", "{}")
			case "late conflict":
				write(".opencode/skills/koard-odd/SKILL.md", "human content")
			case "symlink destination":
				write(".opencode/agents/human.md", "human content")
				if err := os.Symlink("human.md", filepath.Join(root, ".opencode/agents/koard.md")); err != nil {
					t.Fatal(err)
				}
			case "symlink parent":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, ".opencode")); err != nil {
					t.Fatal(err)
				}
			case "install":
				write(".opencode/agents/human.md", "human content")
			}
			code := Run(args, root, &out, &errOut)
			if scenario != "preview" && scenario != "install" {
				if code != 1 || errOut.Len() == 0 {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
				}
				if _, err := os.Lstat(filepath.Join(root, ".opencode/opencode.jsonc")); !os.IsNotExist(err) {
					t.Fatalf("preflight wrote config: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(root, ".opencode/agents/koard-explorer.md")); !os.IsNotExist(err) {
					t.Fatalf("preflight wrote an earlier asset: %v", err)
				}
				if scenario == "conflict" {
					data, _ := os.ReadFile(filepath.Join(root, ".opencode/agents/koard.md"))
					if string(data) != "human content" {
						t.Fatal("overwrote human agent")
					}
				}
				return
			}
			if code != 0 || errOut.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if scenario == "preview" {
				entries, _ := os.ReadDir(root)
				if len(entries) != 0 || !strings.Contains(out.String(), "Would create") {
					t.Fatalf("preview mutated workspace or missing output: %q", out.String())
				}
				return
			}
			for _, path := range []string{".opencode/opencode.jsonc", ".opencode/agents/koard.md", ".opencode/agents/koard-explorer.md", ".opencode/agents/koard-verifier.md", ".opencode/skills/koard-odd/SKILL.md"} {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || len(data) == 0 {
					t.Fatalf("missing asset %s: %v", path, err)
				}
			}
			human, _ := os.ReadFile(filepath.Join(root, ".opencode/agents/human.md"))
			if string(human) != "human content" {
				t.Fatal("changed unrelated file")
			}
			out.Reset()
			if code := Run(args, root, &out, &errOut); code != 0 || !strings.Contains(out.String(), "Unchanged") || strings.Contains(out.String(), "Created") {
				t.Fatalf("rerun: %d %s %s", code, &out, &errOut)
			}
			out.Reset()
			if code := Run([]string{"skill", "validate", "--strict"}, root, &out, &errOut); code != 0 {
				t.Fatalf("installed skill is invalid: %s %s", &out, &errOut)
			}
		})
	}
}

func TestNativeSetupPreservesExistingCommands(t *testing.T) {
	for _, command := range []string{"run", "odd", "review", "memory", "skill", "sync"} {
		var out, errOut bytes.Buffer
		want := 0
		if command == "review" {
			want = 2
		} // Existing review dispatch treats --help as unknown.
		if code := Run([]string{command, "--help"}, t.TempDir(), &out, &errOut); code != want || !strings.Contains(out.String()+errOut.String(), "koard "+command) || (want == 0 && errOut.Len() != 0) {
			t.Fatalf("%s: %d %s %s", command, code, &out, &errOut)
		}
	}
}

func TestNativeSetupArguments(t *testing.T) {
	for _, args := range [][]string{{"native"}, {"native", "setup", "--help"}, {"native", "setup"}, {"native", "setup", "--runtime", "claude"}, {"native", "setup", "--runtime", "opencode", "extra"}} {
		var out, errOut bytes.Buffer
		code := Run(args, t.TempDir(), &out, &errOut)
		want := 2
		if len(args) == 1 || args[len(args)-1] == "--help" {
			want = 0
		}
		if code != want {
			t.Fatalf("%v: got %d, want %d: %s", args, code, want, &errOut)
		}
		if want == 0 && !strings.Contains(out.String(), "koard native setup") {
			t.Fatal("missing help")
		}
	}
}
