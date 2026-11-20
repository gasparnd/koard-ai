package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestVerifierContractMemoryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		role  handoff.Role
		title int
		want  string
	}{
		{"verifier-limit", handoff.RoleVerifier, 256, ""},
		{"verifier-over", handoff.RoleVerifier, 257, "decode Codex proposal result: memory_proposals[0]: title must be nonblank and at most 256 bytes"},
		{"planner", handoff.RolePlanner, 1, "decode Codex proposal result: memory_proposals require a completed verifier result"},
		{"executor", handoff.RoleExecutor, 1, "decode Codex proposal result: memory_proposals require a completed verifier result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.From, req.To = handoff.RoleOrchestrator, tc.role
			ref := handoff.Reference{Kind: "source", URI: "repo://a.go", Digest: "sha256:" + strings.Repeat("a", 64)}
			req.Context = []handoff.Reference{ref}
			result := handoff.Result{SchemaVersion: 1, HandoffID: req.ID, Role: req.To, Status: handoff.StatusCompleted, Summary: "inspected", MemoryProposals: []handoff.MemoryProposal{{Type: "discovery", Title: strings.Repeat("a", tc.title), Content: "observed", Rationale: "useful", Evidence: []handoff.Reference{ref}, Sensitivity: "unknown"}}}
			b, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			runner := promptRunner(t, "codex-cli 0.149.0", b)
			_, err = testAdapter(t, runner).Execute(context.Background(), req)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			assertPromptContains(t, runner, []string{"copy request.to", "at most 256, 4096, and 1024 bytes", "from request.context", "Memory proposals do not authorize persistence"})
		})
	}
}

func TestVerifierContractReviewShape(t *testing.T) {
	req, dir := validReviewRequest(t)
	output := []byte(`{"schema_version":1,"request_id":"review-1","attempt":1,"target":{},"verdict":"rejected","findings":[{"id":"f","severity":"warning","summary":"observed","evidence":[{"kind":"source","uri":"repo://change.go","digest":"x"}]}]}`)
	runner := promptRunner(t, "codex-cli 0.149.0", output)
	_, err := testAdapter(t, runner).Review(context.Background(), req, dir)
	want := `decode Codex review result: json: unknown field "kind"`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v want %s", err, want)
	}
	assertPromptContains(t, runner, []string{"copy the entire request.target object unchanged", "only uri and digest", "correction_required requires at least one finding", "approved must not contain blocking findings"})
}
