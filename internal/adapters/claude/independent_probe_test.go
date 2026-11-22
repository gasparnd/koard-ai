package claude_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/adapters/claude"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.WriteFile(p, []byte(s), 0700); e != nil {
		t.Fatal(e)
	}
}
func enc(v any) string     { b, _ := json.Marshal(v); return string(b) }
func hash(s string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s))) }
func req() handoff.Request {
	return handoff.Request{SchemaVersion: 1, ID: "probe", TaskID: "task", From: handoff.RolePlanner, To: handoff.RoleExecutor, Objective: "Explain explicit evidence only", Capabilities: []string{"repository:read"}, ExpectedOutput: handoff.OutputContract{Format: "application/json"}}
}
func good() string {
	return enc(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": enc(handoff.Result{SchemaVersion: 1, HandoffID: "probe", Role: handoff.RoleExecutor, Status: handoff.StatusBlocked, Summary: "Missing context"})})
}
func native(t *testing.T, version, auth, output, extra string) (*claude.Adapter, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable fixture")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	for n, s := range map[string]string{"version": version, "auth": auth, "output": output} {
		put(t, filepath.Join(root, n), s)
	}
	script := "#!/bin/sh\nset -eu\nR='" + strings.ReplaceAll(root, "'", "'\"'\"'") + "'\n[ -z \"${ANTHROPIC_API_KEY-}\" ] || exit 91\n[ -z \"${CLAUDE_CODE_OAUTH_TOKEN-}\" ] || exit 92\n[ -z \"$(ls -A .)\" ] || exit 93\nprintf '%s\\n' \"$PWD\" >> \"$R/cwd\"\nprintf '%s\\n' \"$@\" >> \"$R/args\"\ncase \"$*\" in\n *--version*) cat \"$R/version\";;\n *'auth status'*) cat \"$R/auth\";;\n *) cat > \"$R/stdin\"\n" + extra + "\ncat \"$R/output\";;\nesac\n"
	exe := filepath.Join(root, "fake-claude")
	put(t, exe, script)
	a, e := claude.New(claude.Config{Executable: exe, Workspace: work, Model: "sonnet"})
	if e != nil {
		t.Fatal(e)
	}
	return a, root, work
}
func TestIndependentNativeProcess(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sentinel-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sentinel-secret")
	for _, tc := range []struct{ name, version, auth, out, extra, want string }{
		{"valid", "2.1.316 (Claude Code)", `{"authMethod":"claude.ai"}`, good(), "", ""},
		{"old", "2.1.315", `{}`, good(), "", "Claude Code 2.1.316 or newer stable 2.x is required"},
		{"future-major", "3.0.0", `{}`, good(), "", "Claude Code 2.1.316 or newer stable 2.x is required"},
		{"api", "2.1.316", `{"authMethod":"api_key"}`, good(), "", "Claude native claude.ai login is required; sign in with your subscription in Claude Code; API fallback is disabled"},
		{"bad-auth", "2.1.316", `{"authMethod":"claude.ai"} {}`, good(), "", "Claude native claude.ai login is required; sign in with your subscription in Claude Code; API fallback is disabled"},
		{"failure", "2.1.316", `{"authMethod":"claude.ai"}`, good(), "echo sentinel-secret >&2; echo sentinel-secret; exit 7", "Claude process failed; check native login, version and managed policy (provider output withheld)"},
		{"envelope", "2.1.316", `{"authMethod":"claude.ai"}`, `{"type":"result","subtype":"success","result":"secret"}`, "", "invalid or unsuccessful Claude result envelope"},
		{"identity", "2.1.316", `{"authMethod":"claude.ai"}`, strings.Replace(good(), "probe", "other", 1), "", "Claude handoff result does not match request"},
		{"strict", "2.1.316", `{"authMethod":"claude.ai"}`, strings.Replace(good(), `Missing context`, `Missing context\",\"extra\":true,\"ignored\":\"`, 1), "", "invalid Claude handoff result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, root, work := native(t, tc.version, tc.auth, tc.out, tc.extra)
			put(t, filepath.Join(work, "sibling"), "unchanged")
			_, e := a.Propose(context.Background(), req())
			got := ""
			if e != nil {
				got = e.Error()
			}
			if got != tc.want {
				t.Fatalf("error=%q want=%q", got, tc.want)
			}
			b, _ := os.ReadFile(filepath.Join(work, "sibling"))
			if hash(string(b)) != hash("unchanged") {
				t.Fatal("sibling changed")
			}
			b, _ = os.ReadFile(filepath.Join(root, "cwd"))
			for _, d := range strings.Fields(string(b)) {
				if _, e := os.Stat(d); !os.IsNotExist(e) {
					t.Fatal("process cwd not cleaned", d)
				}
			}
			if tc.want == "" {
				b, _ = os.ReadFile(filepath.Join(root, "args"))
				for _, s := range []string{"--tools\n\n", "--permission-mode\ndontAsk\n", "--strict-mcp-config\n", "--no-session-persistence\n", "--setting-sources\n\n"} {
					if !strings.Contains(string(b), s) {
						t.Fatal("missing flags", s)
					}
				}
				for _, s := range []string{"--resume", "--dangerously-skip", "--bare"} {
					if strings.Contains(string(b), s) {
						t.Fatal("forbidden flag", s)
					}
				}
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		a, _, _ := native(t, "2.1.316", `{"authMethod":"claude.ai"}`, good(), "exec /bin/sleep 10")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, e := a.Propose(ctx, req())
		if e != context.DeadlineExceeded {
			t.Fatal(e)
		}
	})
}
func TestIndependentContext(t *testing.T) {
	for _, name := range []string{"missing", "digest", "secret-alias", "escaping", "nul", "escaped-size", "one-hop", "64-deduplicated"} {
		t.Run(name, func(t *testing.T) {
			a, root, work := native(t, "2.1.316", `{"authMethod":"claude.ai"}`, good(), "")
			r := req()
			put(t, filepath.Join(work, "source"), "content")
			r.Context = []handoff.Reference{{Kind: "source", URI: "repo://source", Digest: hash("content")}}
			wantFailure := true
			switch name {
			case "missing":
				r.Context[0].URI = "repo://absent"
			case "digest":
				r.Context[0].Digest = hash("different")
			case "secret-alias":
				put(t, filepath.Join(work, ".env"), "secret")
				if e := os.Symlink(".env", filepath.Join(work, "alias")); e != nil {
					t.Fatal(e)
				}
				r.Context[0] = handoff.Reference{Kind: "source", URI: "repo://alias"}
			case "escaping":
				r.Context[0].URI = "repo://../output"
			case "nul":
				put(t, filepath.Join(work, "source"), "a\x00b")
				r.Context[0].Digest = ""
			case "escaped-size":
				put(t, filepath.Join(work, "source"), strings.Repeat("<", 100000))
				r.Context[0].Digest = ""
			case "one-hop":
				wantFailure = false
				prior := handoff.Result{SchemaVersion: 1, HandoffID: "earlier", Role: handoff.RoleExecutor, Status: handoff.StatusCompleted, Summary: "done", Evidence: r.Context}
				s := enc(prior)
				put(t, filepath.Join(work, "prior"), s)
				r.Context = []handoff.Reference{{Kind: "koard.handoff.result", URI: "repo://prior", Digest: hash(s)}}
			case "64-deduplicated":
				wantFailure = false
				r.Context = nil
				for i := 0; i < 64; i++ {
					n := fmt.Sprintf("f%d", i)
					put(t, filepath.Join(work, n), "x")
					r.Context = append(r.Context, handoff.Reference{Kind: "source", URI: "repo://" + n, Digest: hash("x")})
				}
				r.Context = append(r.Context, r.Context[0])
			}
			before, _ := os.ReadFile(filepath.Join(work, "source"))
			_, e := a.Propose(context.Background(), r)
			after, _ := os.ReadFile(filepath.Join(work, "source"))
			if hash(string(before)) != hash(string(after)) {
				t.Fatal("mutated source")
			}
			if wantFailure {
				if e == nil {
					t.Fatal("accepted invalid context")
				}
				if _, e := os.Stat(filepath.Join(root, "args")); !os.IsNotExist(e) {
					t.Fatal("started process on invalid context")
				}
				t.Log(e)
			} else if e != nil {
				t.Fatal(e)
			} else if name == "one-hop" {
				b, _ := os.ReadFile(filepath.Join(root, "stdin"))
				if !strings.Contains(string(b), `"content":"content"`) {
					t.Fatal("evidence not expanded")
				}
			}
		})
	}
}
func TestIndependentFrozenReview(t *testing.T) {
	a, root, work := native(t, "2.1.316", `{"authMethod":"claude.ai"}`, good(), "")
	put(t, filepath.Join(work, "source"), "frozen")
	store := t.TempDir()
	s, e := snapshot.Capture(context.Background(), work, store, "candidate", []handoff.Reference{{Kind: "source", URI: "repo://source"}})
	if e != nil {
		t.Fatal(e)
	}
	r := review.Request{SchemaVersion: 1, ID: "review", Attempt: 1, Target: consent.Target{CandidateID: "candidate", SnapshotID: s.ID}, Snapshot: s.Manifest, Capabilities: []string{"repository:read"}}
	result := review.Result{SchemaVersion: 1, RequestID: r.ID, Attempt: 1, Target: r.Target, Verdict: review.VerdictApproved}
	put(t, filepath.Join(root, "output"), enc(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": enc(result)}))
	put(t, filepath.Join(work, "source"), "live-secret")
	if _, e = a.Review(context.Background(), r, store); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "stdin"))
	if !strings.Contains(string(b), `"content":"frozen"`) || strings.Contains(string(b), "live-secret") {
		t.Fatal("wrong bytes")
	}
	r.Target.CandidateID = "other"
	if _, e = a.Review(context.Background(), r, store); e == nil || e.Error() != "review snapshot does not match target" {
		t.Fatal(e)
	}
}
