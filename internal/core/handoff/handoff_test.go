package handoff

import (
	"bytes"
	"strings"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	t.Parallel()

	request := validRequest()
	encoded, err := MarshalRequest(request)
	if err != nil {
		t.Fatalf("marshal valid request: %v", err)
	}

	decoded, err := DecodeRequest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode valid request: %v", err)
	}

	if decoded.ID != request.ID || decoded.From != RolePlanner || decoded.To != RoleExecutor {
		t.Fatalf("unexpected decoded request: %#v", decoded)
	}
	if decoded.Context[0].Digest != "sha256:candidate" {
		t.Fatalf("expected context digest to survive round trip, got %q", decoded.Context[0].Digest)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*Request)
		expected string
	}{
		{
			name: "missing handoff identity",
			mutate: func(request *Request) {
				request.ID = ""
			},
			expected: "id is required",
		},
		{
			name: "missing task identity",
			mutate: func(request *Request) {
				request.TaskID = ""
			},
			expected: "task_id is required",
		},
		{
			name: "unsupported schema version",
			mutate: func(request *Request) {
				request.SchemaVersion = 2
			},
			expected: "unsupported schema_version 2",
		},
		{
			name: "unknown source role",
			mutate: func(request *Request) {
				request.From = Role("architect")
			},
			expected: "from role",
		},
		{
			name: "same source and destination",
			mutate: func(request *Request) {
				request.To = request.From
			},
			expected: "must be different",
		},
		{
			name: "unknown destination role",
			mutate: func(request *Request) {
				request.To = Role("publisher")
			},
			expected: "to role",
		},
		{
			name: "missing objective",
			mutate: func(request *Request) {
				request.Objective = " "
			},
			expected: "objective is required",
		},
		{
			name: "missing output format",
			mutate: func(request *Request) {
				request.ExpectedOutput.Format = ""
			},
			expected: "expected_output.format is required",
		},
		{
			name: "incomplete context reference",
			mutate: func(request *Request) {
				request.Context[0].URI = ""
			},
			expected: "context[0].uri is required",
		},
		{
			name: "empty capability",
			mutate: func(request *Request) {
				request.Capabilities = append(request.Capabilities, "")
			},
			expected: "capabilities[1] is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := validRequest()
			test.mutate(&request)
			if err := request.Validate(); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestDecodeRequestRejectsUnknownAndTrailingContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unknown field",
			input:    `{"schema_version":1,"id":"handoff-1","task_id":"task-1","from":"planner","to":"executor","objective":"Implement contracts","expected_output":{"format":"application/json"},"authority":true}`,
			expected: "unknown field",
		},
		{
			name:     "multiple JSON values",
			input:    `{"schema_version":1,"id":"handoff-1","task_id":"task-1","from":"planner","to":"executor","objective":"Implement contracts","expected_output":{"format":"application/json"}} {}`,
			expected: "multiple JSON values",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeRequest(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestResultMustBindToRequest(t *testing.T) {
	t.Parallel()

	request := validRequest()
	result := validResult()
	if err := ValidateResultFor(request, result); err != nil {
		t.Fatalf("expected matching result, got %v", err)
	}

	tests := []struct {
		name     string
		mutate   func(*Result)
		expected string
	}{
		{
			name: "missing handoff identity",
			mutate: func(result *Result) {
				result.HandoffID = ""
			},
			expected: "handoff_id is required",
		},
		{
			name: "different handoff identity",
			mutate: func(result *Result) {
				result.HandoffID = "handoff-other"
			},
			expected: "does not match request",
		},
		{
			name: "different responding role",
			mutate: func(result *Result) {
				result.Role = RoleVerifier
			},
			expected: "does not match request destination",
		},
		{
			name: "invalid result status",
			mutate: func(result *Result) {
				result.Status = Status("approved")
			},
			expected: "status",
		},
		{
			name: "missing result summary",
			mutate: func(result *Result) {
				result.Summary = ""
			},
			expected: "summary is required",
		},
		{
			name: "incomplete artifact",
			mutate: func(result *Result) {
				result.Artifacts[0].Kind = ""
			},
			expected: "artifacts[0].kind is required",
		},
		{
			name: "incomplete evidence",
			mutate: func(result *Result) {
				result.Evidence[0].URI = ""
			},
			expected: "evidence[0].uri is required",
		},
		{
			name: "invalid next role",
			mutate: func(result *Result) {
				result.Next = &Transition{Role: Role("publisher"), Reason: "Ship it"}
			},
			expected: "next.role",
		},
		{
			name: "missing next reason",
			mutate: func(result *Result) {
				result.Next = &Transition{Role: RoleVerifier}
			},
			expected: "next.reason is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := validResult()
			test.mutate(&candidate)
			if err := ValidateResultFor(request, candidate); err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestResultRoundTrip(t *testing.T) {
	t.Parallel()

	result := validResult()
	encoded, err := MarshalResult(result)
	if err != nil {
		t.Fatalf("marshal valid result: %v", err)
	}

	decoded, err := DecodeResult(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode valid result: %v", err)
	}
	if decoded.HandoffID != result.HandoffID || decoded.Role != result.Role || decoded.Status != result.Status {
		t.Fatalf("unexpected decoded result: %#v", decoded)
	}
}

func validRequest() Request {
	return Request{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "handoff-1",
		TaskID:        "task-1",
		From:          RolePlanner,
		To:            RoleExecutor,
		Objective:     "Implement provider-neutral contracts",
		Context: []Reference{
			{Kind: "repository", URI: "koard://repository/current", Digest: "sha256:candidate"},
		},
		Capabilities: []string{"repository:read"},
		ExpectedOutput: OutputContract{
			Format: "application/json",
			Schema: "koard://schemas/handoff-result/v1",
		},
	}
}

func validResult() Result {
	return Result{
		SchemaVersion: CurrentSchemaVersion,
		HandoffID:     "handoff-1",
		Role:          RoleExecutor,
		Status:        StatusCompleted,
		Summary:       "Contracts implemented and verified.",
		Artifacts: []Reference{
			{Kind: "source", URI: "repo://internal/core/handoff/handoff.go", Digest: "sha256:artifact"},
		},
		Evidence: []Reference{
			{Kind: "test", URI: "command://go-test", Digest: "sha256:evidence"},
		},
		Next: &Transition{Role: RoleVerifier, Reason: "Verify the candidate."},
	}
}
