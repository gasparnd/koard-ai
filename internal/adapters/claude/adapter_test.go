package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/consent"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

var _ agentrouter.Runner = (*Adapter)(nil)
var _ review.Reviewer = (*Adapter)(nil)

type fakeRunner struct {
	calls         []command
	answer        []byte
	auth, version string
	fail          error
	inspect       func(command)
}

func (f *fakeRunner) Run(ctx context.Context, c command) ([]byte, error) {
	f.calls = append(f.calls, c)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.inspect != nil {
		f.inspect(c)
	}
	if reflect.DeepEqual(c.args, []string{"--version"}) {
		return []byte(f.version), nil
	}
	if strings.Contains(strings.Join(c.args, " "), "auth status") {
		return []byte(f.auth), nil
	}
	return f.answer, f.fail
}
func fixture(t *testing.T) (*Adapter, *fakeRunner, handoff.Request) {
	t.Helper()
	f := &fakeRunner{version: "2.1.316 (Claude Code)", auth: `{"authMethod":"claude.ai"}`}
	a, err := newAdapter(Config{Workspace: t.TempDir(), Model: "sonnet"}, f)
	if err != nil {
		t.Fatal(err)
	}
	r := handoff.Request{SchemaVersion: 1, ID: "h1", TaskID: "t1", From: handoff.RolePlanner, To: handoff.RoleExecutor, Objective: "Explain the supplied material", Capabilities: []string{"repository:read"}, ExpectedOutput: handoff.OutputContract{Format: "application/json"}}
	f.answer = envelope(t, handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Explained"})
	return a, f, r
}
func envelope(t *testing.T, result any) []byte {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": string(b), "session_id": "test"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func write(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestToolFreeProposal(t *testing.T) {
	for _, role := range []handoff.Role{handoff.RolePlanner, handoff.RoleExecutor, handoff.RoleVerifier} {
		t.Run(string(role), func(t *testing.T) {
			a, f, r := fixture(t)
			r.From = handoff.RoleOrchestrator
			r.To = role
			write(t, a.workspace, "source.go", "package example\n")
			r.Context = []handoff.Reference{{Kind: "source", URI: "repo://source.go"}}
			f.answer = envelope(t, handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: role, Status: handoff.StatusCompleted, Summary: "Explained"})
			f.inspect = func(c command) {
				entries, err := os.ReadDir(c.dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("scratch not empty: %v", err)
				}
			}
			got, err := a.Execute(context.Background(), r)
			if err != nil || got.Role != role {
				t.Fatalf("%+v %v", got, err)
			}
			if len(f.calls) != 3 {
				t.Fatalf("calls=%d", len(f.calls))
			}
			c := f.calls[2]
			if !strings.Contains(c.stdin, `package example\n`) || !strings.Contains(c.stdin, "sha256:") {
				t.Fatal("source not packaged")
			}
			for _, pair := range [][2]string{{"--tools", ""}, {"--setting-sources", ""}, {"--permission-mode", "dontAsk"}, {"--model", "sonnet"}} {
				found := false
				for i := 0; i < len(c.args)-1; i++ {
					if c.args[i] == pair[0] && c.args[i+1] == pair[1] {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %v", pair)
				}
			}
			for _, flag := range []string{"--safe-mode", "--restricted", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands"} {
				if !contains(c.args, flag) {
					t.Fatal(flag)
				}
			}
			for _, flag := range []string{"--bare", "--dangerously-skip-permissions", "--resume", "--continue", "--add-dir"} {
				if contains(c.args, flag) {
					t.Fatal(flag)
				}
			}
			if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
				t.Fatal("scratch leaked")
			}
		})
	}
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestProposalRefusesBeforeProcess(t *testing.T) {
	for _, kind := range []string{"capability", "format", "identity", "missing", "escape", "digest", "binary", "secret", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			a, f, r := fixture(t)
			switch kind {
			case "capability":
				r.Capabilities = []string{"workspace:write"}
			case "format":
				r.ExpectedOutput.Format = "text/plain"
			case "identity":
				r.ID = ""
			default:
				name, content := "source.txt", "hello"
				if kind == "escape" {
					name = "../escape"
				}
				if kind == "secret" {
					name = ".env"
				}
				if kind == "binary" {
					content = "a\x00b"
				}
				if kind == "oversized" {
					content = strings.Repeat("x", maxBundleBytes+1)
				}
				if kind != "missing" && kind != "escape" {
					write(t, a.workspace, name, content)
				}
				r.Context = []handoff.Reference{{Kind: "source", URI: "repo://" + name}}
				if kind == "digest" {
					r.Context[0].Digest = "sha256:" + strings.Repeat("0", 64)
				}
			}
			if _, err := a.Execute(context.Background(), r); err == nil {
				t.Fatal("accepted")
			}
			if len(f.calls) != 0 {
				t.Fatal("invoked provider")
			}
		})
	}
}

func TestTransportAndAuthRefusals(t *testing.T) {
	for _, raw := range []string{`{}`, `{"type":"result","subtype":"success","result":"{}"}`, `{"type":"result","subtype":"success","is_error":null,"result":"{}"}`, `{"type":"result","subtype":"error_max_turns","is_error":false,"result":"{}"}`, `{"type":"result","subtype":"success","is_error":true,"result":"{}"}`, `[]`, `{} {}`, "secret provider error", `{"type":"result","subtype":"success","is_error":false,"result":""}`} {
		a, f, r := fixture(t)
		f.answer = []byte(raw)
		if _, err := a.Execute(context.Background(), r); err == nil {
			t.Fatalf("accepted %s", raw)
		} else if strings.Contains(err.Error(), raw) {
			t.Fatal("raw output leaked")
		}
	}
	for _, auth := range []string{`{}`, `{"authMethod":"api_key"}`, `{"authMethod":"oauth_token"}`, `{"authMethod":"claude.ai"} {}`} {
		a, f, r := fixture(t)
		f.auth = auth
		if _, err := a.Execute(context.Background(), r); err == nil || len(f.calls) != 2 {
			t.Fatalf("auth=%s calls=%d err=%v", auth, len(f.calls), err)
		}
	}
	a, f, r := fixture(t)
	f.version = "2.1.315"
	if _, err := a.Execute(context.Background(), r); err == nil || len(f.calls) != 1 {
		t.Fatal("old version accepted")
	}
	a, f, r = fixture(t)
	f.fail = errors.New("private error")
	if _, err := a.Execute(context.Background(), r); err == nil || strings.Contains(err.Error(), "private error") {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.calls[2].dir); !os.IsNotExist(err) {
		t.Fatal("failure leaked scratch")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, f, r = fixture(t)
	if _, err := a.Execute(ctx, r); !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
		t.Fatal(err)
	}
}

func TestReviewPackagesFrozenBytes(t *testing.T) {
	a, f, _ := fixture(t)
	write(t, a.workspace, "source.go", "package original\n")
	dir := t.TempDir()
	s, err := snapshot.Capture(context.Background(), a.workspace, dir, "candidate", []handoff.Reference{{Kind: "source", URI: "repo://source.go"}})
	if err != nil {
		t.Fatal(err)
	}
	r := review.Request{SchemaVersion: 1, ID: "r1", Attempt: 1, Target: consent.Target{CandidateID: "candidate", SnapshotID: s.ID}, Capabilities: []string{"repository:read"}, Snapshot: s.Manifest}
	f.answer = envelope(t, review.Result{SchemaVersion: 1, RequestID: r.ID, Attempt: 1, Target: r.Target, Verdict: review.VerdictApproved})
	write(t, a.workspace, "source.go", "changed live file")
	got, err := a.Review(context.Background(), r, dir)
	if err != nil || got.RequestID != r.ID {
		t.Fatal(err)
	}
	c := f.calls[2]
	if !strings.Contains(c.stdin, `package original\n`) || strings.Contains(c.stdin, "changed live file") {
		t.Fatal("review not frozen")
	}
	f.calls = nil
	r.Snapshot.Entries[0].Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := a.Review(context.Background(), r, dir); err == nil || len(f.calls) != 0 {
		t.Fatal("tamper accepted")
	}
}

func TestChildEnvironment(t *testing.T) {
	got := childEnvironment([]string{"PATH=/bin", "HOME=/home/test", "ANTHROPIC_API_KEY=secret", "ANTHROPIC_AUTH_TOKEN=secret", "CLAUDE_CODE_OAUTH_TOKEN=secret", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CONFIG_DIR=/config", "PWD=/old"})
	for _, entry := range got {
		if strings.Contains(entry, "secret") || strings.Contains(entry, "BEDROCK") {
			t.Fatal("API override retained")
		}
	}
	if !contains(got, "HOME=/home/test") || !contains(got, "CLAUDE_CONFIG_DIR=/config") {
		t.Fatal("native login location lost")
	}
}
