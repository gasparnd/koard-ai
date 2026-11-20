package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// S1: exercise the public adapter with real process exit status and NDJSON I/O.
func TestVerifierOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, operation := range []string{"proposal", "review"} {
		t.Run(operation, func(t *testing.T) {
			request, snapshotDir := validReviewRequest(t)
			payload := string(validResultJSON("handoff-1", handoff.RoleExecutor))
			if operation == "review" {
				b, err := json.Marshal(review.Result{SchemaVersion: 1, RequestID: request.ID, Attempt: request.Attempt, Target: request.Target, Verdict: review.VerdictApproved})
				if err != nil {
					t.Fatal(err)
				}
				payload = string(b)
			}
			step := func(id string, stamp int, text, reason string) string {
				var lines []string
				for _, kind := range []string{"step_start", "text", "step_finish"} {
					part := map[string]any{"sessionID": "s", "messageID": id, "type": strings.ReplaceAll(kind, "_", "-")}
					if kind == "text" {
						part["text"] = text
					}
					if kind == "step_finish" {
						part["reason"] = reason
					}
					b, _ := json.Marshal(map[string]any{"sessionID": "s", "timestamp": stamp, "type": kind, "part": part})
					lines = append(lines, string(b))
				}
				return strings.Join(lines, "\n") + "\n"
			}
			good := step("new", 20, payload, "stop")
			action := "run OpenCode " + operation
			for _, tc := range []struct{ name, stream, tail, want string }{
				{"no-fallback", step("old", 10, payload, "stop") + step("new", 20, "I cannot answer", "stop"), "", "decode OpenCode " + operation + " result: " + map[string]string{"proposal": "decode handoff result: ", "review": ""}[operation] + "invalid character 'I' looking for beginning of value"},
				{"late-old", good + step("old", 10, "old prose", "tool-calls"), "", ""},
				{"tie", good + step("old", 20, payload, "stop"), "", action + " transport: ambiguous OpenCode step order"},
				{"latest-empty", step("old", 10, payload, "stop") + step("new", 20, "", "stop"), "", action + " transport: OpenCode has no completed final text"},
				{"latest-tool", step("old", 10, payload, "stop") + step("new", 20, payload, "tool-calls"), "", action + " transport: OpenCode has no completed final text"},
				{"late-error", good + "{\"sessionID\":\"s\",\"type\":\"error\",\"error\":\"PRIVATE\"}\n", "", action + " transport: OpenCode reported a run error"},
				{"process-error", good, "echo deterministic-failure >&2; exit 7\n", action + ": deterministic-failure: exit status 7"},
				{"incomplete-old", good + "{\"sessionID\":\"s\",\"timestamp\":1,\"type\":\"step_start\",\"part\":{\"sessionID\":\"s\",\"messageID\":\"old\",\"type\":\"step-start\"}}\n", "", action + " transport: incomplete OpenCode step"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := t.TempDir()
					marker := filepath.Join(dir, "existing")
					if err := os.WriteFile(marker, []byte("unchanged"), 0600); err != nil {
						t.Fatal(err)
					}
					before, _ := os.ReadFile(marker)
					hash := sha256.Sum256(before)
					stream := filepath.Join(dir, "stream")
					if err := os.WriteFile(stream, []byte(tc.stream), 0600); err != nil {
						t.Fatal(err)
					}
					exe := filepath.Join(dir, "provider")
					script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.2.6'; exit 0; fi\nprintf '%s' \"$PWD\" > '" + dir + "/cwd'\ncat '" + stream + "'\n" + tc.tail
					if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
						t.Fatal(err)
					}
					a, err := New(Config{Executable: exe, Workspace: dir, Model: "test/model", Agent: "explicit"})
					if err != nil {
						t.Fatal(err)
					}
					if operation == "proposal" {
						_, err = a.Propose(context.Background(), validRequest())
					} else {
						_, err = a.Review(context.Background(), request, snapshotDir)
					}
					got := ""
					if err != nil {
						got = err.Error()
					}
					if got != tc.want {
						t.Fatalf("want %q; got %q", tc.want, got)
					}
					after, _ := os.ReadFile(marker)
					if sha256.Sum256(after) != hash {
						t.Fatal("workspace changed")
					}
					if operation == "review" {
						cwd, e := os.ReadFile(filepath.Join(dir, "cwd"))
						if e != nil {
							t.Fatal(e)
						}
						if _, e = os.Stat(string(cwd)); !os.IsNotExist(e) {
							t.Fatalf("review directory remains: %s", cwd)
						}
					}
				})
			}
			t.Run("canceled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				a, err := New(Config{Executable: "/bin/sh", Workspace: t.TempDir(), Model: "test/model"})
				if err != nil {
					t.Fatal(err)
				}
				if operation == "proposal" {
					_, err = a.Propose(ctx, validRequest())
				} else {
					_, err = a.Review(ctx, request, snapshotDir)
				}
				if err == nil || err.Error() != "check OpenCode version: context canceled" {
					t.Fatalf("unexpected cancellation: %v", err)
				}
			})
		})
	}
}
