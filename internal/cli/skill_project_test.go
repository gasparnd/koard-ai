package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/skillregistry"
)

func TestSkillProject(t *testing.T) {
	for _, runtime := range []string{"opencode", "codex"} {
		t.Run(runtime, func(t *testing.T) {
			root := t.TempDir()
			config := skillFile(t, root, ".koard/koard.yaml", "preserve configuration")
			source := skillFile(t, root, "skills/testing/SKILL.md", compatibleSkill)
			skillFile(t, root, "skills/testing/references/example.md", "original reference")
			destRoot := ".opencode/skills"
			if runtime == "codex" {
				destRoot = ".agents/skills"
			}
			dest := filepath.Join(root, destRoot, "testing")
			args := []string{"skill", "project", "--runtime", runtime}
			code, out, diagnostic := skillCall(t, root, append(args, "--dry-run", "testing")...)
			if code != 0 || diagnostic != "" || !strings.Contains(out, "Would project") {
				t.Fatalf("%d %s %s", code, out, diagnostic)
			}
			if _, err := os.Lstat(filepath.Join(root, strings.Split(destRoot, "/")[0])); !os.IsNotExist(err) {
				t.Fatal("dry run wrote destination")
			}
			code, out, diagnostic = skillCall(t, root, append(args, "testing")...)
			if code != 0 || diagnostic != "" || !strings.Contains(out, "Projected") {
				t.Fatalf("%d %s %s", code, out, diagnostic)
			}
			link, err := os.Readlink(dest)
			if err != nil || filepath.IsAbs(link) {
				t.Fatalf("relative directory link required: %q %v", link, err)
			}
			for _, path := range []string{"SKILL.md", "references/example.md"} {
				want, err := os.ReadFile(filepath.Join(filepath.Dir(source), path))
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(dest, path))
				if err != nil || string(got) != string(want) {
					t.Fatalf("content %s: %v", path, err)
				}
			}
			code, out, diagnostic = skillCall(t, root, append(args, "testing")...)
			if code != 0 || diagnostic != "" || !strings.Contains(out, "Already projected") {
				t.Fatalf("replay: %d %s %s", code, out, diagnostic)
			}
			updated := strings.Replace(compatibleSkill, "Verify observed behavior", "Verify updated behavior", 1)
			if err := os.WriteFile(source, []byte(updated), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
			if err != nil || string(got) != updated {
				t.Fatal("projection detached from source")
			}
			code, out, diagnostic = skillCall(t, root, "skill", "list", "--json")
			if code != 0 || diagnostic != "" || !strings.Contains(out, `"code": "shadowed"`) {
				t.Fatalf("discovery regression: %d %s %s", code, out, diagnostic)
			}
			_, sources, err := skillregistry.Capture(root, "")
			if err != nil || len(sources) != 1 || sources[0].Content != updated {
				t.Fatalf("capture changed: %v %v", sources, err)
			}
			b, err := os.ReadFile(config)
			if err != nil || string(b) != "preserve configuration" {
				t.Fatal("configuration changed")
			}
			moved := filepath.Join(t.TempDir(), "moved-project")
			if err := os.Rename(root, moved); err != nil {
				t.Fatal(err)
			}
			b, err = os.ReadFile(filepath.Join(moved, destRoot, "testing/SKILL.md"))
			if err != nil || string(b) != updated {
				t.Fatalf("project relocation broke link: %v", err)
			}
		})
	}
}

func TestSkillProjectNativeSourceIsNoop(t *testing.T) {
	root := t.TempDir()
	source := skillFile(t, root, ".agents/skills/testing/SKILL.md", compatibleSkill)
	code, out, diagnostic := skillCall(t, root, "skill", "project", "--runtime", "codex", "testing")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "Already projected") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	b, err := os.ReadFile(source)
	if err != nil || string(b) != compatibleSkill {
		t.Fatal("native source changed")
	}
}

func TestSkillProjectRefusals(t *testing.T) {
	for _, runtime := range []string{"opencode", "codex"} {
		for _, kind := range []string{"directory", "file", "foreign-link", "dangling-link", "parent-link"} {
			t.Run(runtime+"/"+kind, func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				source := skillFile(t, root, "skills/testing/SKILL.md", compatibleSkill)
				parent := ".opencode"
				if runtime == "codex" {
					parent = ".agents"
				}
				dest := filepath.Join(root, parent, "skills/testing")
				if kind == "parent-link" {
					if err := os.Symlink(outside, filepath.Join(root, parent)); err != nil {
						t.Skip(err)
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
						t.Fatal(err)
					}
					switch kind {
					case "directory":
						if err := os.Mkdir(dest, 0755); err != nil {
							t.Fatal(err)
						}
					case "file":
						if err := os.WriteFile(dest, []byte("keep"), 0600); err != nil {
							t.Fatal(err)
						}
					case "foreign-link":
						if err := os.Symlink(outside, dest); err != nil {
							t.Skip(err)
						}
					case "dangling-link":
						if err := os.Symlink(filepath.Join(outside, "absent"), dest); err != nil {
							t.Skip(err)
						}
					}
				}
				for _, dry := range []bool{true, false} {
					args := []string{"skill", "project", "--runtime", runtime}
					if dry {
						args = append(args, "--dry-run")
					}
					args = append(args, "testing")
					code, out, diagnostic := skillCall(t, root, args...)
					if code != 1 || out != "" || diagnostic == "" {
						t.Fatalf("%d %s %s", code, out, diagnostic)
					}
				}
				b, err := os.ReadFile(source)
				if err != nil || string(b) != compatibleSkill {
					t.Fatal("source modified")
				}
				entries, err := os.ReadDir(outside)
				if err != nil || len(entries) != 0 {
					t.Fatal("external files written")
				}
				if kind == "file" {
					b, err := os.ReadFile(dest)
					if err != nil || string(b) != "keep" {
						t.Fatal("conflict overwritten")
					}
				}
			})
		}
	}
}

func TestSkillProjectArgumentsAndInactiveSources(t *testing.T) {
	for _, args := range [][]string{{}, {"testing"}, {"--runtime", "other", "testing"}, {"--runtime", "codex", "../escape"}, {"--runtime", "codex", "testing", "--dry-run"}, {"--global", "testing"}, {"--runtime", "codex", "--runtime", "opencode", "testing"}} {
		root := t.TempDir()
		code, out, diagnostic := skillCall(t, root, append([]string{"skill", "project"}, args...)...)
		if code != 2 || out != "" || diagnostic == "" {
			t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatal("usage wrote files")
		}
	}
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	skillFile(t, root, ".koard/skill-drafts/skills/testing/SKILL.md", compatibleSkill)
	skillFile(t, home, ".agents/skills/testing/SKILL.md", compatibleSkill)
	code, out, diagnostic := skillCall(t, root, "skill", "project", "--runtime", "codex", "testing")
	if code != 1 || out != "" || !strings.Contains(diagnostic, "not found") {
		t.Fatalf("inactive/global projected: %d %s %s", code, out, diagnostic)
	}
	code, out, diagnostic = skillCall(t, root, "skill", "project", "--help")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "--runtime") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
}
