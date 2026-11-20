package opencode

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
)

// Envelopes follow OpenCode v2.2.3/v2.2.6 run/noninteractive.ts, not model JSON.
func event(kind, message string, stamp int, value string) []byte {
	part := map[string]any{"sessionID": "session-1", "messageID": message, "type": strings.ReplaceAll(kind, "_", "-")}
	if kind == "text" {
		part["text"] = value
	}
	if kind == "step_finish" {
		part["reason"] = value
	}
	b, _ := json.Marshal(map[string]any{"type": kind, "sessionID": "session-1", "timestamp": stamp, "part": part})
	return append(b, '\n')
}

func transcript(payload []byte) []byte {
	b := event("step_start", "final", 2, "")
	b = append(b, event("text", "final", 3, string(payload))...)
	return append(b, event("step_finish", "final", 4, "stop")...)
}

func TestAdapterEventTransport(t *testing.T) {
	for _, operation := range []string{"proposal", "review"} {
		t.Run(operation, func(t *testing.T) {
			r, dir := validReviewRequest(t)
			payload := validResultJSON("handoff-1", handoff.RoleExecutor)
			if operation == "review" {
				payload = mustMarshalReviewResult(t, review.Result{SchemaVersion: 1, RequestID: r.ID, Attempt: r.Attempt, Target: r.Target, Verdict: review.VerdictApproved})
			}
			valid := transcript(payload)
			interim := event("step_start", "earlier", 1, "")
			interim = append(interim, event("text", "earlier", 1, "I will inspect the file.")...)
			interim = append(interim, event("step_finish", "earlier", 1, "tool-calls")...)
			late := append(append([]byte{}, valid...), interim...)
			split := event("step_start", "final", 2, "")
			split = append(split, event("text", "final", 3, string(payload[:len(payload)/2]))...)
			split = append(split, event("step_finish", "final", 4, "stop")...)
			split = append(split, event("text", "final", 5, string(payload[len(payload)/2:]))...)
			for _, tc := range []struct {
				name string
				data []byte
				ok   bool
			}{
				{"single", valid, true}, {"interim", append(append([]byte{}, interim...), valid...), true},
				{"late older reconciliation", late, true}, {"suffix after finish", split, true},
				{"bare model JSON", payload, false}, {"empty", nil, false}, {"malformed", []byte("I cannot comply"), false},
				{"missing finish", event("step_start", "final", 2, ""), false},
				{"late error", append(append([]byte{}, valid...), []byte("{\"type\":\"error\",\"sessionID\":\"session-1\"}\n")...), false},
				{"mixed sessions", []byte(strings.Replace(string(valid), `"sessionID":"session-1"`, `"sessionID":"other"`, 1)), false},
				{"invalid final after valid interim", append(append(event("step_start", "earlier", 1, ""), event("text", "earlier", 1, string(payload))...), append(event("step_finish", "earlier", 1, "tool-calls"), transcript([]byte("I cannot comply"))...)...), false},
				{"truncated finish", valid[:len(valid)-3], false},
				{"length finish", []byte(strings.Replace(string(valid), `"reason":"stop"`, `"reason":"length"`, 1)), false},
				{"missing text", []byte(strings.Replace(string(valid), `"text":`, `"other":`, 1)), false},
				{"reasoning only", []byte(strings.ReplaceAll(string(valid), `"type":"text"`, `"type":"reasoning"`)), false},
				{"fenced final", transcript([]byte("```json\n" + string(payload) + "\n```")), false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := &fakeRunner{responses: []commandResult{{stdout: []byte("opencode v2.2.6")}, {stdout: tc.data}}}
					a := testAdapter(t, runner)
					var err error
					if operation == "proposal" {
						var got handoff.Result
						got, err = a.Propose(context.Background(), validRequest())
						if err == nil && got.HandoffID != "handoff-1" {
							t.Fatal(got)
						}
					} else {
						var got review.Result
						got, err = a.Review(context.Background(), r, dir)
						if err == nil && got.RequestID != r.ID {
							t.Fatal(got)
						}
					}
					if (err == nil) != tc.ok {
						t.Fatalf("success=%v, err=%v", tc.ok, err)
					}
					if len(runner.commands) != 2 {
						t.Fatal("unexpected retry")
					}
					if !strings.Contains(strings.Join(runner.commands[1].Args, " "), "--format json") {
						t.Fatal("missing framed output flag")
					}
					if operation == "review" {
						if _, err := os.Stat(runner.commands[1].Dir); !os.IsNotExist(err) {
							t.Fatal("review materialization leaked")
						}
					}
				})
			}
		})
	}
}

func TestPromptsSupplyResultContracts(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		fields []string
	}{
		{proposalPrompt("REQUEST"), []string{"schema_version", "handoff_id", "role", "status", "summary", "artifacts", "evidence", "next", "memory_proposals", "completed", "blocked", "failed"}},
		{reviewPrompt("REQUEST"), []string{"schema_version", "request_id", "attempt", "target", "verdict", "findings", "approved", "correction_required", "rejected"}},
	} {
		for _, field := range tc.fields {
			if !strings.Contains(tc.prompt, field) {
				t.Errorf("missing contract field %s", field)
			}
		}
	}
}
