package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRejectsDanglingSkillDestination(t *testing.T) {
	for _, dry := range []bool{false, true} {
		name := "install"
		if dry {
			name = "preview"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			dest := filepath.Join(root, ".opencode/skills/koard-odd/SKILL.md")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(outside, "missing")
			if err := os.Symlink(target, dest); err != nil {
				t.Fatal(err)
			}
			args := []string{"native", "setup", "--runtime", "opencode"}
			if dry {
				args = append(args, "--dry-run")
			}
			var out, stderr bytes.Buffer
			code := Run(args, root, &out, &stderr)
			want := "koard native: " + filepath.Join(".opencode", "skills", "koard-odd", "SKILL.md") + ": destination conflicts; no files are overwritten\n"
			if code != 1 || out.Len() != 0 || stderr.String() != want {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
			if got, err := os.Readlink(dest); err != nil || got != target {
				t.Fatalf("symlink changed: %q %v", got, err)
			}
			for _, path := range []string{target, filepath.Join(root, ".opencode/opencode.jsonc"), filepath.Join(root, ".opencode/agents")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("unexpected mutation %s: %v", path, err)
				}
			}
		})
	}
}
