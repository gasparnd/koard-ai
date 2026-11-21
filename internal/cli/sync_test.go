package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syncConfig(t *testing.T, root, extra string) string {
	t.Helper()
	return skillFile(t, root, ".koard/koard.yaml", defaultConfig+extra)
}

const explicitSync = `
sync:
  skills:
    - name: testing
      runtimes: [opencode, codex]
    - name: other
      runtimes: [codex]
`

func TestSyncExplicitList(t *testing.T) {
	root := t.TempDir()
	config := syncConfig(t, root, explicitSync)
	for _, name := range []string{"testing", "other", "unselected"} {
		skillFile(t, root, "skills/"+name+"/SKILL.md", strings.Replace(compatibleSkill, "name: testing", "name: "+name, 1))
	}
	code, out, diagnostic := skillCall(t, root, "sync", "--dry-run")
	if code != 0 || diagnostic != "" || strings.Count(out, "Would project") != 3 || !strings.Contains(out, "Sync preview: 3 to create, 0 unchanged.") {
		t.Fatalf("preview: %d %s %s", code, out, diagnostic)
	}
	for _, dir := range []string{".agents", ".opencode"} {
		if _, err := os.Lstat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			t.Fatal("preview wrote files")
		}
	}
	code, out, diagnostic = skillCall(t, root, "sync")
	if code != 0 || diagnostic != "" || strings.Count(out, "Projected") != 3 || !strings.Contains(out, "Sync complete: 3 created, 0 unchanged.") {
		t.Fatalf("apply: %d %s %s", code, out, diagnostic)
	}
	links := []string{".opencode/skills/testing", ".agents/skills/testing", ".agents/skills/other"}
	before := make(map[string]string)
	for _, path := range links {
		link, err := os.Readlink(filepath.Join(root, path))
		if err != nil || filepath.IsAbs(link) {
			t.Fatalf("invalid link: %s %v", link, err)
		}
		before[path] = link
		b, err := os.ReadFile(filepath.Join(root, path, "SKILL.md"))
		if err != nil || !strings.Contains(string(b), "Verify observed behavior") {
			t.Fatalf("source unreadable: %v", err)
		}
	}
	for _, path := range []string{".opencode/skills/other", ".opencode/skills/unselected", ".agents/skills/unselected"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("unlisted projection: %s", path)
		}
	}
	for _, args := range [][]string{{"sync"}, {"sync", "--dry-run"}} {
		code, out, diagnostic = skillCall(t, root, args...)
		if code != 0 || diagnostic != "" || strings.Count(out, "Already projected") != 3 || !strings.Contains(out, "3 unchanged.") {
			t.Fatalf("replay: %d %s %s", code, out, diagnostic)
		}
		for path, want := range before {
			link, err := os.Readlink(filepath.Join(root, path))
			if err != nil || link != want {
				t.Fatal("replay replaced link")
			}
		}
	}
	b, err := os.ReadFile(config)
	if err != nil || string(b) != defaultConfig+explicitSync {
		t.Fatal("configuration changed")
	}
	code, out, diagnostic = skillCall(t, root, "doctor")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "schema version 1") {
		t.Fatalf("doctor: %d %s %s", code, out, diagnostic)
	}
	// Removing a declaration is not permission to delete an existing link.
	syncConfig(t, root, "\nsync:\n  skills: []\n")
	code, out, diagnostic = skillCall(t, root, "sync")
	if code != 0 || diagnostic != "" || out != "No skills configured for sync.\n" {
		t.Fatalf("empty: %d %s %s", code, out, diagnostic)
	}
	for path, want := range before {
		link, err := os.Readlink(filepath.Join(root, path))
		if err != nil || link != want {
			t.Fatal("removed declaration deleted link")
		}
	}
}

func TestSyncPreflightBeforeAnyWrite(t *testing.T) {
	for _, kind := range []string{"missing", "conflict", "runtime", "duplicate-skill", "duplicate-runtime", "missing-runtime", "unknown-field", "invalid-name"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			skillFile(t, root, "skills/testing/SKILL.md", compatibleSkill)
			extra := "\nsync:\n  skills:\n    - name: testing\n      runtimes: [opencode]\n    - name: other\n      runtimes: [codex]\n"
			switch kind {
			case "conflict":
				skillFile(t, root, "skills/other/SKILL.md", strings.Replace(compatibleSkill, "name: testing", "name: other", 1))
				skillFile(t, root, ".agents/skills/other", "keep")
			case "runtime":
				extra = strings.Replace(extra, "[codex]", "[unsupported]", 1)
			case "duplicate-skill":
				extra = strings.Replace(extra, "name: other", "name: testing", 1)
			case "duplicate-runtime":
				extra = strings.Replace(extra, "[codex]", "[codex, codex]", 1)
			case "missing-runtime":
				extra = strings.Replace(extra, "[codex]", "[]", 1)
			case "unknown-field":
				extra += "      unknown: true\n"
			case "invalid-name":
				extra = strings.Replace(extra, "name: other", "name: ../escape", 1)
			}
			config := syncConfig(t, root, extra)
			for _, args := range [][]string{{"sync", "--dry-run"}, {"sync"}} {
				code, out, diagnostic := skillCall(t, root, args...)
				if code != 1 || out != "" || diagnostic == "" {
					t.Fatalf("%d %s %s", code, out, diagnostic)
				}
				if _, err := os.Lstat(filepath.Join(root, ".opencode")); !os.IsNotExist(err) {
					t.Fatal("preflight failure wrote earlier item")
				}
			}
			b, err := os.ReadFile(config)
			if err != nil || string(b) != defaultConfig+extra {
				t.Fatal("invalid configuration changed")
			}
			if kind == "conflict" {
				b, err := os.ReadFile(filepath.Join(root, ".agents/skills/other"))
				if err != nil || string(b) != "keep" {
					t.Fatal("conflict overwritten")
				}
			}
		})
	}
}

func TestSyncUsageAndLegacyConfiguration(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"sync", "extra"}, {"sync", "--runtime", "codex"}, {"sync", "--global"}, {"sync", "--force"}} {
		code, out, diagnostic := skillCall(t, root, args...)
		if code != 2 || out != "" || diagnostic == "" {
			t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
		}
	}
	code, out, diagnostic := skillCall(t, root, "sync", "--help")
	if code != 0 || diagnostic != "" || !strings.Contains(out, "koard sync [--dry-run]") {
		t.Fatalf("%d %s %s", code, out, diagnostic)
	}
	code, out, diagnostic = skillCall(t, root, "sync")
	if code != 1 || out != "" || !strings.Contains(diagnostic, "koard init") {
		t.Fatalf("missing config: %d %s %s", code, out, diagnostic)
	}
	syncConfig(t, root, "")
	code, out, diagnostic = skillCall(t, root, "sync")
	if code != 0 || diagnostic != "" || out != "No skills configured for sync.\n" {
		t.Fatalf("legacy config: %d %s %s", code, out, diagnostic)
	}
	code, out, diagnostic = skillCall(t, root, "doctor")
	if code != 0 || diagnostic != "" || out != "Koard project configuration is valid (schema version 1).\n" {
		t.Fatalf("legacy doctor changed: %d %s %s", code, out, diagnostic)
	}
}
