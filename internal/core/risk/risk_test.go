package risk

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

// capture stores files as a real snapshot and returns its directory and ID.
func capture(t *testing.T, files map[string]string, order ...string) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	artifacts := make([]handoff.Reference, 0, len(order))
	for _, name := range order {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, handoff.Reference{Kind: "source", URI: "repo://" + name})
	}
	dir := filepath.Join(t.TempDir(), "snapshot")
	snap, err := snapshot.Capture(context.Background(), workspace, dir, "sha256:candidate", artifacts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	return dir, snap.ID
}

func reasons(assessment Assessment) []string {
	codes := make([]string, 0, len(assessment.Reasons))
	for _, reason := range assessment.Reasons {
		if reason.Path != "" {
			codes = append(codes, reason.Code+":"+reason.Path)
		} else {
			codes = append(codes, reason.Code)
		}
	}
	return codes
}

func TestAssessTiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		files   map[string]string
		order   []string
		tier    Tier
		reasons []string
	}{
		{"markdown", map[string]string{"docs/a.md": "# A\n"}, []string{"docs/a.md"}, Passive, []string{"non_executable_only"}},
		{"markdown and rst", map[string]string{"a.md": "# A\n", "b.rst": "B\n"}, []string{"a.md", "b.rst"}, Passive, []string{"non_executable_only"}},
		{"no artifacts", nil, nil, Passive, []string{"non_executable_only"}},
		{"plain text", map[string]string{"notes.txt": "x\n"}, []string{"notes.txt"}, Medium, []string{"executable_change:notes.txt"}},
		{"image", map[string]string{"docs/i.png": "\x89PNG"}, []string{"docs/i.png"}, Medium, []string{"executable_change:docs/i.png"}},
		{"go code", map[string]string{"internal/a.go": "package a\n"}, []string{"internal/a.go"}, Medium, []string{"executable_change:internal/a.go"}},
		{"go module", map[string]string{"go.mod": "module x\n"}, []string{"go.mod"}, Medium, []string{"configuration_change:go.mod"}},
		{"json", map[string]string{"cfg.json": "{}"}, []string{"cfg.json"}, Medium, []string{"configuration_change:cfg.json"}},
		{"dockerfile", map[string]string{"Dockerfile": "FROM alpine\n"}, []string{"Dockerfile"}, Medium, []string{"configuration_change:Dockerfile"}},
		{"makefile", map[string]string{"Makefile": "all:\n"}, []string{"Makefile"}, Medium, []string{"configuration_change:Makefile"}},
		{"network and filesystem stay medium", map[string]string{"a.go": "package a\n\nimport (\n\t\"net/http\"\n\t\"os\"\n)\n\nvar _, _ = http.Get, os.RemoveAll\n"}, []string{"a.go"}, Medium, []string{"executable_change:a.go"}},
		{"docs do not add reasons to medium", map[string]string{"a.md": "# A\n", "a.go": "package a\n"}, []string{"a.md", "a.go"}, Medium, []string{"executable_change:a.go"}},
		{"ci workflow", map[string]string{".github/workflows/ci.yml": "on: push\n"}, []string{".github/workflows/ci.yml"}, High, []string{"shell_source:.github/workflows/ci.yml"}},
		{"shell script", map[string]string{"install.sh": "#!/bin/sh\necho hi\n"}, []string{"install.sh"}, High, []string{"process_boundary:install.sh", "shell_source:install.sh"}},
		{"shebang without extension", map[string]string{"bin/run": "#!/usr/bin/env bash\n"}, []string{"bin/run"}, High, []string{"process_boundary:bin/run", "shell_source:bin/run"}},
		{"go process", map[string]string{"internal/b.go": "package a\n\nimport \"os/exec\"\n\nvar _ = exec.Command\n"}, []string{"internal/b.go"}, High, []string{"process_boundary:internal/b.go"}},
		{"go start process", map[string]string{"c.go": "package a\n\nimport \"os\"\n\nvar _ = os.StartProcess\n"}, []string{"c.go"}, High, []string{"process_boundary:c.go"}},
		{"hot path file", map[string]string{"internal/auth.go": "package a\n"}, []string{"internal/auth.go"}, High, []string{"hot_path:internal/auth.go"}},
		{"hot path directory", map[string]string{"internal/security/x.go": "package security\n"}, []string{"internal/security/x.go"}, High, []string{"hot_path:internal/security/x.go"}},
		{"high lists only high reasons", map[string]string{"a.md": "# A\n", "a.go": "package a\n", "secrets.yaml": "k: v\n"}, []string{"a.md", "a.go", "secrets.yaml"}, High, []string{"hot_path:secrets.yaml"}},
		{"duplicate artifacts reported once", map[string]string{"internal/auth.go": "package a\n"}, []string{"internal/auth.go", "internal/auth.go"}, High, []string{"hot_path:internal/auth.go"}},
		{"size does not raise the tier", map[string]string{"a.go": "package a\n" + strings.Repeat("// line\n", 3000)}, []string{"a.go"}, Medium, []string{"executable_change:a.go"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			dir, id := capture(t, testCase.files, testCase.order...)

			assessment, err := Assess(dir, id, nil)
			if err != nil {
				t.Fatalf("assess: %v", err)
			}
			if assessment.Tier != testCase.tier {
				t.Fatalf("tier is %q, expected %q (reasons %v)", assessment.Tier, testCase.tier, reasons(assessment))
			}
			if got := reasons(assessment); !reflect.DeepEqual(got, testCase.reasons) {
				t.Fatalf("reasons are %v, expected %v", got, testCase.reasons)
			}
			if assessment.ReviewDue != (testCase.tier == High) {
				t.Fatalf("review_due is %v for tier %q", assessment.ReviewDue, assessment.Tier)
			}
			if assessment.SnapshotID != id || assessment.SchemaVersion != CurrentSchemaVersion {
				t.Fatalf("assessment is not bound to the snapshot: %#v", assessment)
			}
		})
	}
}

func TestAssessEscalationOnlyRaises(t *testing.T) {
	t.Parallel()

	dir, id := capture(t, map[string]string{"a.md": "# A\n"}, "a.md")
	assessment, err := Assess(dir, id, []Escalation{{Item: 2, Reason: "touches a guard"}})
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if assessment.Tier != High || !assessment.ReviewDue {
		t.Fatalf("an escalation did not raise passive to high: %#v", assessment)
	}
	if len(assessment.Reasons) != 1 || assessment.Reasons[0].Code != "agent_escalation" ||
		!strings.Contains(assessment.Reasons[0].Detail, "item 2") || !strings.Contains(assessment.Reasons[0].Detail, "touches a guard") {
		t.Fatalf("the escalation reason does not name the item and reason: %#v", assessment.Reasons)
	}

	// An escalation added to a high candidate keeps every high reason.
	dir, id = capture(t, map[string]string{"install.sh": "#!/bin/sh\n"}, "install.sh")
	assessment, err = Assess(dir, id, []Escalation{{Item: 5, Reason: "installer"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := reasons(assessment); !reflect.DeepEqual(got, []string{"process_boundary:install.sh", "shell_source:install.sh", "agent_escalation"}) {
		t.Fatalf("unexpected reasons: %v", got)
	}
}

func TestAssessRejectsInvalidEscalation(t *testing.T) {
	t.Parallel()

	dir, id := capture(t, map[string]string{"a.md": "# A\n"}, "a.md")
	for _, escalation := range []Escalation{{Item: 0, Reason: "x"}, {Item: 7, Reason: "x"}, {Item: 2, Reason: "  "}} {
		if _, err := Assess(dir, id, []Escalation{escalation}); err == nil {
			t.Errorf("escalation %#v was accepted", escalation)
		}
	}
}

func TestAssessUnverifiableSnapshotIsHigh(t *testing.T) {
	t.Parallel()

	dir, id := capture(t, map[string]string{"a.md": "# A\n"}, "a.md")
	for name, assess := range map[string]func() (Assessment, error){
		"wrong identity":    func() (Assessment, error) { return Assess(dir, "sha256:other", nil) },
		"missing directory": func() (Assessment, error) { return Assess(filepath.Join(t.TempDir(), "none"), id, nil) },
	} {
		assessment, err := assess()
		if err != nil {
			t.Fatalf("%s: an unverifiable snapshot must assess, not fail: %v", name, err)
		}
		if assessment.Tier != High || !assessment.ReviewDue {
			t.Fatalf("%s: an unverifiable snapshot was not high: %#v", name, assessment)
		}
		if len(assessment.Reasons) != 1 || assessment.Reasons[0].Code != "unassessable" || assessment.Reasons[0].Detail == "" {
			t.Fatalf("%s: unexpected reasons: %#v", name, assessment.Reasons)
		}
	}
}
