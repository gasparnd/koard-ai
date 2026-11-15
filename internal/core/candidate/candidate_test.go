package candidate

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

func TestProduceIsDeterministicForTheSameProposal(t *testing.T) {
	t.Parallel()

	request := validRequest()
	result := validResult(request)

	first, err := Produce(request, result)
	if err != nil {
		t.Fatalf("produce candidate: %v", err)
	}
	second, err := Produce(request, result)
	if err != nil {
		t.Fatalf("produce candidate twice: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("the same proposal produced %q and %q", first.ID, second.ID)
	}
	if first.ID == "" {
		t.Fatal("candidate identity is empty")
	}
}

func TestProduceIgnoresProvenance(t *testing.T) {
	t.Parallel()

	request := validRequest()
	result := validResult(request)
	first, err := Produce(request, result)
	if err != nil {
		t.Fatalf("produce candidate: %v", err)
	}

	other := validRequest()
	other.ID = "workflow-9-7"
	other.TaskID = "task-9"
	otherResult := validResult(other)
	second, err := Produce(other, otherResult)
	if err != nil {
		t.Fatalf("produce candidate from another run: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("identical content produced different identities: %q and %q", first.ID, second.ID)
	}
	if first.TaskID == second.TaskID || first.HandoffID == second.HandoffID {
		t.Fatalf("provenance was not recorded: %#v and %#v", first, second)
	}
}

func TestProduceChangesIdentityWithContent(t *testing.T) {
	t.Parallel()

	request := validRequest()
	base, err := Produce(request, validResult(request))
	if err != nil {
		t.Fatalf("produce candidate: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*handoff.Result)
	}{
		{name: "summary", mutate: func(result *handoff.Result) { result.Summary = "Delete the configuration field." }},
		{name: "artifact digest", mutate: func(result *handoff.Result) { result.Artifacts[0].Digest = "sha256:b" }},
		{name: "artifact uri", mutate: func(result *handoff.Result) { result.Artifacts[0].URI = "koard://proposals/task-2" }},
		{name: "added evidence", mutate: func(result *handoff.Result) {
			result.Evidence = []handoff.Reference{{Kind: "test", URI: "koard://evidence/1"}}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := validResult(request)
			test.mutate(&result)
			changed, err := Produce(request, result)
			if err != nil {
				t.Fatalf("produce candidate: %v", err)
			}
			if changed.ID == base.ID {
				t.Fatalf("changing the %s did not change the identity", test.name)
			}
		})
	}
}

func TestProduceRejectsNonCompletedResult(t *testing.T) {
	t.Parallel()

	for _, status := range []handoff.Status{handoff.StatusBlocked, handoff.StatusFailed} {
		request := validRequest()
		result := validResult(request)
		result.Status = status

		if _, err := Produce(request, result); err == nil || !strings.Contains(err.Error(), "completed") {
			t.Fatalf("status %q must not produce a candidate, got %v", status, err)
		}
	}
}

func TestProduceRejectsUnboundResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*handoff.Result)
		expected string
	}{
		{name: "foreign handoff", mutate: func(result *handoff.Result) { result.HandoffID = "workflow-1-2" }, expected: "does not match request"},
		{name: "foreign role", mutate: func(result *handoff.Result) { result.Role = handoff.RolePlanner }, expected: "does not match request destination"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := validRequest()
			result := validResult(request)
			test.mutate(&result)

			if _, err := Produce(request, result); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestProduceIdentityDependsOnArtifactOrder(t *testing.T) {
	t.Parallel()

	request := validRequest()
	first := validResult(request)
	first.Artifacts = []handoff.Reference{
		{Kind: "proposal", URI: "koard://proposals/a", Digest: "sha256:a"},
		{Kind: "proposal", URI: "koard://proposals/b", Digest: "sha256:b"},
	}
	second := first
	second.Artifacts = []handoff.Reference{first.Artifacts[1], first.Artifacts[0]}

	firstCandidate, err := Produce(request, first)
	if err != nil {
		t.Fatalf("produce candidate: %v", err)
	}
	secondCandidate, err := Produce(request, second)
	if err != nil {
		t.Fatalf("produce reordered candidate: %v", err)
	}
	if firstCandidate.ID == secondCandidate.ID {
		t.Fatal("artifact order is part of the proposal and must change the identity")
	}
}

func TestCandidateIdentityFormat(t *testing.T) {
	t.Parallel()

	request := validRequest()
	produced, err := Produce(request, validResult(request))
	if err != nil {
		t.Fatalf("produce candidate: %v", err)
	}
	if !strings.HasPrefix(produced.ID, IDPrefix) {
		t.Fatalf("identity %q does not start with %q", produced.ID, IDPrefix)
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(produced.ID, IDPrefix))
	if err != nil {
		t.Fatalf("identity %q is not hex encoded: %v", produced.ID, err)
	}
	if len(digest) != 32 {
		t.Fatalf("identity carries %d digest bytes, expected 32", len(digest))
	}
}

func validRequest() handoff.Request {
	return handoff.Request{
		SchemaVersion:  handoff.CurrentSchemaVersion,
		ID:             "workflow-1-1",
		TaskID:         "task-1",
		From:           handoff.RolePlanner,
		To:             handoff.RoleExecutor,
		Objective:      "Propose the requested change without modifying the workspace.",
		Capabilities:   []string{"repository:read"},
		ExpectedOutput: handoff.OutputContract{Format: "application/json"},
	}
}

func validResult(request handoff.Request) handoff.Result {
	return handoff.Result{
		SchemaVersion: handoff.CurrentSchemaVersion,
		HandoffID:     request.ID,
		Role:          request.To,
		Status:        handoff.StatusCompleted,
		Summary:       "Rename the configuration field.",
		Artifacts: []handoff.Reference{
			{Kind: "proposal", URI: "koard://proposals/task-1", Digest: "sha256:a"},
		},
	}
}
