package skillsync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/application/skillsync"
)

type target string

func (t target) SkillDirectory() string { return string(t) }

// Introduce an ordinary conflicting file only after successful preflight.
type lateConflict struct {
	root  string
	calls int
	t     *testing.T
}

func (c *lateConflict) SkillDirectory() string {
	c.calls++
	if c.calls == 2 {
		p := filepath.Join(c.root, ".agents/skills/second")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			c.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("human file"), 0600); err != nil {
			c.t.Fatal(err)
		}
	}
	return ".agents/skills"
}

func TestRunPartialFailureAndRecovery(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first", "second"} {
		p := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test\n---\nInstructions.\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	requests := []skillsync.Request{{Name: "first", Target: target(".opencode/skills")}, {Name: "second", Target: &lateConflict{root: root, t: t}}}
	results, err := skillsync.Run(root, requests, false)
	if err == nil || !strings.Contains(err.Error(), "stopped after 1 of 2") || len(results) != 1 || results[0].Existing {
		t.Fatalf("partial outcome: %+v %v", results, err)
	}
	first := filepath.Join(root, ".opencode/skills/first")
	link, err := os.Readlink(first)
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(root, ".agents/skills/second")
	b, err := os.ReadFile(conflict)
	if err != nil || string(b) != "human file" {
		t.Fatal("conflict overwritten")
	}
	// The human resolves only the conflicting fixture, then retries.
	if err := os.Remove(conflict); err != nil {
		t.Fatal(err)
	}
	requests[1].Target = target(".agents/skills")
	results, err = skillsync.Run(root, requests, false)
	if err != nil || len(results) != 2 || !results[0].Existing || results[1].Existing {
		t.Fatalf("recovery: %+v %v", results, err)
	}
	after, err := os.Readlink(first)
	if err != nil || after != link {
		t.Fatal("recovery replaced successful link")
	}
}
