package oddworkflow_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/application/oddworkflow"
	"github.com/koard-ai/koard/internal/config"
)

func TestPreparationValidatesAndPublishesOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".koard"), 0700); err != nil {
		t.Fatal(err)
	}
	text := "schema_version: 1\nproject:\n  name: example\nworkflow:\n  default: odd\nmemory:\n  provider: none\nagents:\n"
	for _, role := range []string{"planner", "executor", "verifier"} {
		text += "  " + role + ":\n    provider: codex\n    model: test\n"
	}
	path := filepath.Join(root, ".koard", "koard.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	p, err := oddworkflow.Prepare(context.Background(), root, "demo", "task", "Goal", "planned", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Machine.State != "implement" || p.Machine.Revision != 4 || len(p.Roles) != 3 {
		t.Fatal(p)
	}
	dir := filepath.Join(root, "demo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.Initialize(root, dir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "odd.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Initialize(root, dir); err == nil {
		t.Fatal("replaced initial preparation")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "odd.json"))
	if string(before) != string(after) {
		t.Fatal("changed preparation")
	}
	if err := os.WriteFile(path, []byte(text+"# drift\n"), 0600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.Initialize(root, other); err == nil {
		t.Fatal("configuration changed before claim")
	}
	if entries, err := os.ReadDir(other); err != nil || len(entries) != 0 {
		t.Fatal("failed preparation published files")
	}
}
