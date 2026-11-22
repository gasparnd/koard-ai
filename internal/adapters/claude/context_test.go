package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestPriorResultEvidenceAndSkillDelivery(t *testing.T) {
	a, f, r := fixture(t)
	write(t, a.workspace, "source.go", "package provided\n")
	write(t, a.workspace, "unrelated.txt", "DO NOT DELIVER")
	previous := handoff.Result{SchemaVersion: 1, HandoffID: "previous", Role: handoff.RoleExecutor, Status: handoff.StatusCompleted, Summary: "Observed source", Artifacts: []handoff.Reference{{Kind: "source", URI: "repo://source.go"}}, Evidence: []handoff.Reference{{Kind: "source", URI: "repo://source.go"}}}
	b, err := handoff.MarshalResult(previous)
	if err != nil {
		t.Fatal(err)
	}
	write(t, a.workspace, "previous.json", string(b))
	r.Context = []handoff.Reference{{Kind: agentrouter.ResultReferenceKind, URI: "repo://previous.json", Digest: digest(b)}}
	r.Objective += "\nFrozen skill instructions: use the supplied bytes only."
	_, err = a.Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	prompt := f.calls[2].stdin
	if strings.Count(prompt, `package provided\n`) != 1 || strings.Contains(prompt, "DO NOT DELIVER") || !strings.Contains(prompt, "Frozen skill instructions") {
		t.Fatal("incorrect bounded bundle")
	}
	// Even a typed nested result remains data: expansion stops after one hop.
	previous.Artifacts = []handoff.Reference{{Kind: agentrouter.ResultReferenceKind, URI: "repo://nested.json"}}
	previous.Evidence = nil
	write(t, a.workspace, "nested.json", `{"artifacts":[{"kind":"source","uri":"repo://missing.txt"}]}`)
	b, _ = handoff.MarshalResult(previous)
	write(t, a.workspace, "previous.json", string(b))
	r.Context[0].Digest = digest(b)
	if _, err = a.Execute(context.Background(), r); err != nil {
		t.Fatal("recursed into nested evidence", err)
	}
	// Selection instructions contain catalog metadata, not paths to discover.
	r.Context = nil
	r.Objective = "Select zero or more skills from supplied catalog koard-skill:example. Return no skills when unrelated."
	f.calls = nil
	if _, err = a.Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.calls[2].stdin, "package provided") {
		t.Fatal("implicit file discovery")
	}
}

func TestUnsafeAliasesAndLimits(t *testing.T) {
	for _, kind := range []string{"external", "credential-alias", "count", "serialized", "invalid-utf8", "contradictory-digest"} {
		t.Run(kind, func(t *testing.T) {
			a, f, r := fixture(t)
			write(t, a.workspace, "source.txt", "safe")
			r.Context = []handoff.Reference{{Kind: "source", URI: "repo://source.txt"}}
			switch kind {
			case "external", "credential-alias":
				target := filepath.Join(t.TempDir(), "outside")
				if kind == "credential-alias" {
					target = filepath.Join(a.workspace, ".env")
				}
				if err := os.WriteFile(target, []byte("sensitive"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(a.workspace, "alias.txt")); err != nil {
					t.Skip(err)
				}
				r.Context[0].URI = "repo://alias.txt"
			case "count":
				for i := 0; i < 65; i++ {
					name := fmt.Sprintf("file%d.txt", i)
					write(t, a.workspace, name, "safe")
					r.Context = append(r.Context, handoff.Reference{Kind: "source", URI: "repo://" + name})
				}
			case "serialized":
				r.Objective = strings.Repeat("\n", maxBundleBytes/2)
			case "invalid-utf8":
				write(t, a.workspace, "source.txt", string([]byte{255}))
			case "contradictory-digest":
				r.Context = append(r.Context, handoff.Reference{Kind: "source", URI: "repo://source.txt", Digest: "sha256:" + strings.Repeat("0", 64)})
			}
			if _, err := a.Execute(context.Background(), r); err == nil || len(f.calls) != 0 {
				t.Fatal("unsafe context invoked provider", err)
			}
		})
	}
}

func TestStrictDomainResults(t *testing.T) {
	for _, payload := range []string{
		`{"schema_version":1,"handoff_id":"wrong","role":"executor","status":"completed","summary":"ok"}`,
		`{"schema_version":1,"handoff_id":"h1","role":"planner","status":"completed","summary":"ok"}`,
		`{"schema_version":1,"handoff_id":"h1","role":"executor","status":"completed","summary":"ok","unknown":true}`,
		`{"schema_version":1,"handoff_id":"h1","role":"executor","status":"completed","summary":"ok"} {}`,
		"```json\n{}\n```",
	} {
		a, f, r := fixture(t)
		f.answer, _ = json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": payload})
		if _, err := a.Execute(context.Background(), r); err == nil {
			t.Fatal("accepted invalid result")
		}
	}
}

func TestOutputBound(t *testing.T) {
	var b cappedBuffer
	p := []byte(strings.Repeat("x", maxOutputBytes+1))
	n, err := b.Write(p)
	if n != len(p) || err != nil || !b.overflow || b.Len() != maxOutputBytes {
		t.Fatal("output not bounded")
	}
	a, f, r := fixture(t)
	f.answer = p
	if _, err := a.Execute(context.Background(), r); err == nil {
		t.Fatal("large output accepted")
	}
}
