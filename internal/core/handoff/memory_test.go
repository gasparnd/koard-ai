package handoff_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func memoryResultJSON(t *testing.T, change func(map[string]any, map[string]any)) string {
	t.Helper()
	p := map[string]any{
		"type": "discovery", "title": "Repository boundary", "content": "Core imports no provider adapters.",
		"rationale": "Preserves the architecture boundary in future changes.", "sensitivity": "unknown",
		"evidence":    []any{map[string]any{"kind": "result", "uri": "repo://result.json", "digest": "sha256:" + strings.Repeat("a", 64)}},
		"uncertainty": []string{"The declaration has not been independently checked."},
	}
	r := map[string]any{"schema_version": 1, "handoff_id": "h1", "role": "verifier", "status": "completed", "summary": "Inspected the proposal.", "memory_proposals": []any{p}}
	if change != nil {
		change(r, p)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMemoryProposalRoundTrip(t *testing.T) {
	input := memoryResultJSON(t, nil)
	result, err := handoff.DecodeResult(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := handoff.MarshalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal([]byte(input), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("proposal changed during round trip: %s", encoded)
	}
}

func TestMemoryProposalValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any, map[string]any)
		want   string
	}{
		{"executor", func(r, p map[string]any) { r["role"] = "executor" }, "completed verifier"},
		{"planner", func(r, p map[string]any) { r["role"] = "planner" }, "completed verifier"},
		{"blocked", func(r, p map[string]any) { r["status"] = "blocked" }, "completed verifier"},
		{"failed", func(r, p map[string]any) { r["status"] = "failed" }, "completed verifier"},
		{"missing title", func(r, p map[string]any) { delete(p, "title") }, "title"},
		{"blank content", func(r, p map[string]any) { p["content"] = " " }, "content"},
		{"large content", func(r, p map[string]any) { p["content"] = strings.Repeat("a", 4097) }, "content"},
		{"missing rationale", func(r, p map[string]any) { delete(p, "rationale") }, "rationale"},
		{"unknown type", func(r, p map[string]any) { p["type"] = "log" }, "type"},
		{"missing sensitivity", func(r, p map[string]any) { delete(p, "sensitivity") }, "sensitivity"},
		{"invalid sensitivity", func(r, p map[string]any) { p["sensitivity"] = "safe" }, "sensitivity"},
		{"no evidence", func(r, p map[string]any) { p["evidence"] = []any{} }, "evidence"},
		{"no digest", func(r, p map[string]any) {
			p["evidence"] = []any{map[string]any{"kind": "result", "uri": "repo://result.json"}}
		}, "digest"},
		{"invalid digest", func(r, p map[string]any) {
			p["evidence"] = []any{map[string]any{"kind": "result", "uri": "repo://result.json", "digest": "sha256:" + strings.Repeat("g", 64)}}
		}, "digest"},
		{"blank uncertainty", func(r, p map[string]any) { p["uncertainty"] = []string{" "} }, "uncertainty"},
		{"too many proposals", func(r, p map[string]any) {
			ps := make([]any, 9)
			for i := range ps {
				ps[i] = p
			}
			r["memory_proposals"] = ps
		}, "at most 8"},
		{"model project", func(r, p map[string]any) { p["project"] = "other" }, "unknown field"},
		{"model permission", func(r, p map[string]any) { p["authorized"] = true }, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handoff.DecodeResult(strings.NewReader(memoryResultJSON(t, tt.change)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMemoryProposalBinding(t *testing.T) {
	result, err := handoff.DecodeResult(strings.NewReader(memoryResultJSON(t, nil)))
	if err != nil {
		t.Fatal(err)
	}
	reference := handoff.Reference{Kind: "result", URI: "repo://result.json", Digest: "sha256:" + strings.Repeat("a", 64)}
	request := handoff.Request{SchemaVersion: 1, ID: "h1", TaskID: "t1", From: handoff.RoleExecutor, To: handoff.RoleVerifier, Objective: "Inspect", Context: []handoff.Reference{reference}, ExpectedOutput: handoff.OutputContract{Format: "json"}}
	if err := handoff.ValidateResultFor(request, result); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"absent", "kind", "uri", "digest"} {
		t.Run(field, func(t *testing.T) {
			changed := request
			ref := reference
			switch field {
			case "kind":
				ref.Kind = "other"
			case "uri":
				ref.URI = "repo://other.json"
			case "digest":
				ref.Digest = "sha256:" + strings.Repeat("b", 64)
			}
			changed.Context = []handoff.Reference{ref}
			if field == "absent" {
				changed.Context = nil
			}
			if err := handoff.ValidateResultFor(changed, result); err == nil || !strings.Contains(err.Error(), "request context") {
				t.Fatalf("unbound proposal accepted: %v", err)
			}
		})
	}
}

func TestResultsWithoutMemoryKeepExistingBehavior(t *testing.T) {
	for _, role := range []handoff.Role{handoff.RolePlanner, handoff.RoleExecutor, handoff.RoleVerifier} {
		for _, status := range []handoff.Status{handoff.StatusCompleted, handoff.StatusBlocked, handoff.StatusFailed} {
			r := handoff.Result{SchemaVersion: 1, HandoffID: "h1", Role: role, Status: status, Summary: "Existing result"}
			b, err := handoff.MarshalResult(r)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "memory_proposals") {
				t.Fatalf("optional field emitted: %s", b)
			}
			got, err := handoff.DecodeResult(strings.NewReader(string(b)))
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatalf("legacy result changed: %+v %v", got, err)
			}
		}
	}
}

func TestMemoryProposalExactLimits(t *testing.T) {
	for _, field := range []string{"title", "uncertainty", "memory_proposals"} {
		for _, overflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/overflow=%v", field, overflow), func(t *testing.T) {
				want := ""
				input := memoryResultJSON(t, func(r, p map[string]any) {
					n := 8
					if overflow {
						n++
					}
					switch field {
					case "title":
						p[field] = strings.Repeat("é", 128)
						if overflow {
							p[field] = p[field].(string) + "x"
							want = "memory_proposals[0]: title must be nonblank and at most 256 bytes"
						}
					case "uncertainty":
						reasons := make([]string, n)
						for i := range reasons {
							reasons[i] = "Needs verification"
						}
						p[field] = reasons
						if overflow {
							want = "memory_proposals[0]: uncertainty permits at most 8 reasons"
						}
					case "memory_proposals":
						proposals := make([]any, n)
						for i := range proposals {
							proposals[i] = p
						}
						r[field] = proposals
						if overflow {
							want = "memory_proposals permits at most 8 proposals"
						}
					}
				})
				result, err := handoff.DecodeResult(strings.NewReader(input))
				got := ""
				if err != nil {
					got = err.Error()
				}
				if got != want {
					t.Fatalf("got %q; want %q", got, want)
				}
				if err == nil {
					if _, err := handoff.MarshalResult(result); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
