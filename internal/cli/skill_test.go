package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillFile(t *testing.T, root, path, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func skillCall(t *testing.T, cwd string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := Run(args, cwd, &out, &err)
	return code, out.String(), err.String()
}

const compatibleSkill = "---\nname: testing\ndescription: >-\n  Trigger: tests.\n  Verify observed behavior.\ncustom: accepted\n---\n# Instructions\nRun the project's tests.\n"

func TestSkillDiscovery(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	skillFile(t, root, "skills/testing/SKILL.md", compatibleSkill)
	skillFile(t, root, ".claude/skills/testing/SKILL.md", compatibleSkill)
	skillFile(t, root, ".opencode/skills/category/other/SKILL.md", strings.Replace(compatibleSkill, "name: testing", "name: other", 1))
	skillFile(t, root, "skills/testing/references/ignored/SKILL.md", compatibleSkill)
	skillFile(t, root, "skills/category/deeper/ignored/SKILL.md", compatibleSkill)
	skillFile(t, home, ".agents/skills/global/SKILL.md", strings.Replace(compatibleSkill, "name: testing", "name: global", 1))
	for _, global := range []bool{false, true} {
		args := []string{"skill", "list", "--json"}
		if global {
			args = append(args, "--global")
		}
		code, out, err := skillCall(t, root, args...)
		if code != 0 || err != "" {
			t.Fatalf("code=%d out=%s err=%s", code, out, err)
		}
		var report struct {
			Skills      []struct{ Name, Description, Path, Scope string }
			Diagnostics []struct{ Code, Severity, Path string }
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatal(err)
		}
		want := 2
		if global {
			want = 3
		}
		if len(report.Skills) != want || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "shadowed" {
			t.Fatalf("report=%+v", report)
		}
		for _, s := range report.Skills {
			if s.Name == "testing" && (s.Path != filepath.Join(root, "skills/testing/SKILL.md") || s.Scope != "project" || s.Description != "Trigger: tests. Verify observed behavior.") {
				t.Fatalf("winner=%+v", s)
			}
			if s.Name == "global" && s.Scope != "global" {
				t.Fatalf("scope=%+v", s)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".koard")); !os.IsNotExist(err) {
		t.Fatal("discovery wrote Koard state")
	}
	if _, err := os.Stat(filepath.Join(root, ".atl")); !os.IsNotExist(err) {
		t.Fatal("discovery wrote shared index")
	}
}

func TestSkillValidation(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		strict     bool
		want       int
		diagnostic string
	}{
		{"compatible", compatibleSkill, false, 0, ""},
		{"fallback name", strings.Replace(compatibleSkill, "name: testing\n", "", 1), false, 0, ""},
		{"missing description", "---\nname: test\n---\nInstructions\n", false, 1, "description"},
		{"invalid YAML", "---\nname: [\n---\nInstructions\n", false, 1, "frontmatter"},
		{"duplicate key", "---\nname: a\nname: b\n---\nInstructions\n", false, 1, "frontmatter"},
		{"empty body", "---\nname: test\ndescription: test\n---\n", false, 1, "body"},
		{"oversized", strings.Repeat("x", 65537), false, 1, "size"},
		{"strict rejects third party", compatibleSkill, true, 1, "authoring"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := skillFile(t, root, "skills/testing/SKILL.md", tt.body)
			args := []string{"skill", "validate"}
			if tt.strict {
				args = append(args, "--strict")
			}
			code, out, err := skillCall(t, root, args...)
			if code != tt.want || !strings.Contains(out+err, tt.diagnostic) {
				t.Fatalf("code=%d out=%s err=%s", code, out, err)
			}
			b, e := os.ReadFile(p)
			if e != nil || string(b) != tt.body {
				t.Fatal("validation mutated skill")
			}
		})
	}
}

func TestSkillBoundaries(t *testing.T) {
	for _, kind := range []string{"file", "directory", "root"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			target := skillFile(t, outside, "secret/SKILL.md", compatibleSkill)
			link := filepath.Join(root, "skills/testing/SKILL.md")
			if kind == "directory" {
				target = filepath.Dir(target)
				link = filepath.Dir(link)
			}
			if kind == "root" {
				target = outside
				link = filepath.Join(root, "skills")
			}
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skip(err)
			}
			code, out, err := skillCall(t, root, "skill", "list", "--json")
			if code != 1 || !strings.Contains(out+err, "outside selected roots") {
				t.Fatalf("code=%d out=%s err=%s", code, out, err)
			}
		})
	}
}

func TestSkillEmptyAndArguments(t *testing.T) {
	root := t.TempDir()
	code, out, err := skillCall(t, root, "skill", "list", "--json")
	if code != 0 || err != "" || !strings.Contains(out, `"skills": []`) || !strings.Contains(out, `"diagnostics": []`) {
		t.Fatalf("code=%d out=%s err=%s", code, out, err)
	}
	for _, args := range [][]string{{"skill", "bad"}, {"skill", "list", "--strict"}, {"skill", "validate", "--unknown"}, {"skill", "list", "extra"}} {
		code, _, err := skillCall(t, root, args...)
		if code != 2 || err == "" {
			t.Fatalf("args=%v code=%d err=%s", args, code, err)
		}
	}
	for _, args := range [][]string{{"skill"}, {"skill", "--help"}, {"skill", "list", "--help"}, {"skill", "validate", "--help"}} {
		code, out, err := skillCall(t, root, args...)
		if code != 0 || err != "" || !strings.Contains(out, "koard skill") {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out, err)
		}
	}
	code, out, err = skillCall(t, root, "version", "--global")
	if code != 0 || out != Version+"\n" || err != "" {
		t.Fatal("existing version behavior changed")
	}
	code, out, err = skillCall(t, root, "doctor", "--global")
	if code != 1 || out != "" || !strings.Contains(err, "run 'koard init'") {
		t.Fatal("existing doctor behavior changed")
	}
}

func TestSkillAuthoringAndShadowedErrors(t *testing.T) {
	root := t.TempDir()
	strict := "---\nname: testing\ndescription: 'Trigger: testing. Verify behavior.'\nlicense: Apache-2.0\nmetadata:\n  author: koard-project\n  version: '1.0'\n---\n"
	for _, heading := range []string{"Activation Contract", "Hard Rules", "Decision Gates", "Execution Steps", "Output Contract", "References"} {
		strict += "## " + heading + "\nFollow the supplied task contract.\n"
	}
	skillFile(t, root, "skills/testing/SKILL.md", strict)
	code, out, err := skillCall(t, root, "skill", "validate", "--strict")
	if code != 0 || err != "" || !strings.Contains(out, "Validated 1 skills.") {
		t.Fatalf("code=%d out=%s err=%s", code, out, err)
	}
	skillFile(t, root, ".claude/skills/testing/SKILL.md", "---\nname: testing\n---\nInstructions\n")
	code, out, err = skillCall(t, root, "skill", "validate", "--json")
	if code != 1 || err != "" || !strings.Contains(out, "description is required") || !strings.Contains(out, ".claude") {
		t.Fatalf("shadowed error hidden: code=%d out=%s err=%s", code, out, err)
	}
}

func TestSkillGlobalSymlinkOptIn(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	target := skillFile(t, home, ".agents/skills/testing/SKILL.md", compatibleSkill)
	link := filepath.Join(root, "skills/testing")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Skip(err)
	}
	code, out, err := skillCall(t, root, "skill", "validate")
	if code != 1 || !strings.Contains(out+err, "outside selected roots") {
		t.Fatalf("implicit global read: %d %s %s", code, out, err)
	}
	code, out, err = skillCall(t, root, "skill", "list", "--global", "--json")
	if code != 0 || err != "" || !strings.Contains(out, `"scope": "project"`) || !strings.Contains(out, `"code": "shadowed"`) {
		t.Fatalf("authorized symlink: %d %s %s", code, out, err)
	}
}

func TestSkillNonregularAndDanglingFiles(t *testing.T) {
	for _, kind := range []string{"directory", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "skills/testing/SKILL.md")
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err := os.Mkdir(p, 0755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(root, "absent"), p); err != nil {
				t.Skip(err)
			}
			code, out, err := skillCall(t, root, "skill", "validate", "--json")
			if code != 1 || err != "" || !strings.Contains(out, `"code": "file"`) {
				t.Fatalf("code=%d out=%s err=%s", code, out, err)
			}
		})
	}
}
