package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/skillregistry"
)

func TestSkillCreateDraft(t *testing.T) {
	root := t.TempDir()
	code, out, diagnostic := skillCall(t, root, "skill", "create", "test-guide")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "Draft created") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	drafts := filepath.Join(root, ".koard/skill-drafts")
	dir := filepath.Join(drafts, "skills/test-guide")
	before := map[string]string{}
	for _, name := range []string{"SKILL.md", "README.md"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(b)
		if !strings.Contains(string(b), "TODO") {
			t.Fatalf("missing editable placeholders: %s", name)
		}
	}
	for _, heading := range []string{"Purpose", "When to use", "Inputs and outputs", "Example", "Limits", "Review and activate"} {
		if !strings.Contains(before["README.md"], "## "+heading) {
			t.Fatalf("missing onboarding: %s", heading)
		}
	}
	code, out, diagnostic = skillCall(t, drafts, "skill", "validate", "--strict")
	if code != 0 || diagnostic != "" || !strings.HasSuffix(out, "Validated 1 skills.\n") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	for _, command := range []string{"list", "validate"} {
		code, out, diagnostic = skillCall(t, root, "skill", command, "--json")
		if code != 0 || diagnostic != "" || !strings.Contains(out, `"skills": []`) {
			t.Fatalf("draft discovered: %d %s %s", code, out, diagnostic)
		}
	}
	_, sources, err := skillregistry.Capture(root, "")
	if err != nil || len(sources) != 0 {
		t.Fatalf("draft captured: %v %v", sources, err)
	}
	second := t.TempDir()
	code, _, diagnostic = skillCall(t, second, "skill", "create", "test-guide")
	if code != 0 || diagnostic != "" {
		t.Fatalf("second creation: %d %s", code, diagnostic)
	}
	for name, want := range before {
		b, err := os.ReadFile(filepath.Join(second, ".koard/skill-drafts/skills/test-guide", name))
		if err != nil || string(b) != want {
			t.Fatalf("nondeterministic %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".koard/koard.yaml")); !os.IsNotExist(err) {
		t.Fatal("creation wrote configuration")
	}
	code, out, diagnostic = skillCall(t, root, "skill", "create", "test-guide")
	if code != 1 || out != "" || diagnostic == "" {
		t.Fatalf("overwrite accepted: %d %s %s", code, out, diagnostic)
	}
	for name, want := range before {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != want {
			t.Fatalf("existing %s changed: %v", name, err)
		}
	}
	// Moving the complete reviewed directory is the documented manual activation.
	if err := os.Mkdir(filepath.Join(root, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(root, "skills/test-guide")); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic = skillCall(t, root, "skill", "validate", "--strict")
	if code != 0 || diagnostic != "" || !strings.HasSuffix(out, "Validated 1 skills.\n") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	_, sources, err = skillregistry.Capture(root, "")
	if err != nil || len(sources) != 1 {
		t.Fatalf("activation: %v %v", sources, err)
	}
}

func TestSkillCreateArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"../escape"}, {"Upper"}, {"a/b"}, {`a\b`}, {"a", "b"}, {"--global", "a"}, {"--json"}, {"a--b"}, {""}, {strings.Repeat("a", 65)}} {
		root := t.TempDir()
		code, out, diagnostic := skillCall(t, root, append([]string{"skill", "create"}, args...)...)
		if code != 2 || out != "" || diagnostic == "" {
			t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("invalid arguments wrote files: %v", err)
		}
	}
	root := t.TempDir()
	code, out, diagnostic := skillCall(t, root, "skill", "create", "--help")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "koard skill create <name>") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
}

func TestSkillCreateRefusesExistingEntries(t *testing.T) {
	for _, path := range []string{".koard", ".koard/skill-drafts", ".koard/skill-drafts/skills", ".koard/skill-drafts/skills/example"} {
		for _, kind := range []string{"file", "symlink", "dangling"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				p := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "file" {
					if err := os.WriteFile(p, []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					target := outside
					if kind == "dangling" {
						target = filepath.Join(outside, "missing")
					}
					if err := os.Symlink(target, p); err != nil {
						t.Skip(err)
					}
				}
				code, out, diagnostic := skillCall(t, root, "skill", "create", "example")
				if code != 1 || out != "" || diagnostic == "" {
					t.Fatalf("%d %s %s", code, out, diagnostic)
				}
				entries, err := os.ReadDir(outside)
				if err != nil || len(entries) != 0 {
					t.Fatalf("wrote outside workspace: %v", err)
				}
				if kind == "file" {
					b, err := os.ReadFile(p)
					if err != nil || string(b) != "keep" {
						t.Fatal("existing file changed")
					}
				}
			})
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".koard/skill-drafts/skills/example")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic := skillCall(t, root, "skill", "create", "example")
	if code != 1 || out != "" || diagnostic == "" {
		t.Fatalf("existing directory accepted: %d %s %s", code, out, diagnostic)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("existing directory modified")
	}
}
