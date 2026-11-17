package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/memory"
)

type cliMemory struct {
	cwd     string
	creates int
	recall  bool
	fail    bool
	lost    bool
}

func (s *cliMemory) Resolve(context.Context, string) (memory.Project, error) {
	return memory.Project{Name: "canonical", Directory: s.cwd}, nil
}
func (s *cliMemory) Search(_ context.Context, p, q string) ([]memory.Observation, error) {
	if p != "canonical" {
		return nil, errors.New("wrong scope")
	}
	if s.fail {
		return nil, errors.New("offline")
	}
	if s.recall && q == "Goal" {
		return []memory.Observation{{ID: 9, Project: p, Scope: "project", Type: "architecture", Title: "Prior knowledge", Content: "Core uses ports.", State: "active"}}, nil
	}
	return nil, nil
}
func (s *cliMemory) Create(_ context.Context, p, op string, v handoff.MemoryProposal) (memory.Observation, error) {
	if p != "canonical" || op == "" {
		return memory.Observation{}, errors.New("bad write binding")
	}
	s.creates++
	if s.fail || s.lost {
		return memory.Observation{}, errors.New("unknown")
	}
	return memory.Observation{ID: 11, Project: p, Scope: "project", Type: v.Type, Title: v.Title, Content: v.Content, State: "active"}, nil
}
func (*cliMemory) Close(context.Context) error { return nil }

func memoryFixture(t *testing.T, sensitivity string, uncertain bool) (string, *cliMemory) {
	t.Helper()
	cwd := workflowProject(t)
	cwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(cwd, ".koard", "koard.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, bytes.ReplaceAll(data, []byte("provider: none"), []byte("provider: engram")), 0600); err != nil {
		t.Fatal(err)
	}
	s := &cliMemory{cwd: cwd}
	oldMemory, oldProvider := memoryProvider, workflowProvider
	t.Cleanup(func() { memoryProvider = oldMemory; workflowProvider = oldProvider })
	memoryProvider = func(context.Context, string) (memoryClient, error) { return s, nil }
	workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
		return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
			out := handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Verified proposal"}
			if s.recall {
				found := false
				for _, ref := range r.Context {
					if ref.Kind == "memory" {
						found = true
						body, err := os.ReadFile(filepath.Join(cwd, strings.TrimPrefix(ref.URI, "repo://")))
						if err != nil || !bytes.Contains(body, []byte("advisory")) {
							t.Fatalf("recall %s %v", body, err)
						}
					}
				}
				if !found {
					t.Error("recall missing from role context")
				}
			}
			if r.To == handoff.RoleVerifier && sensitivity != "none" {
				var evidence handoff.Reference
				for _, ref := range r.Context {
					if ref.Kind == agentrouter.ResultReferenceKind {
						evidence = ref
						break
					}
				}
				p := handoff.MemoryProposal{Type: "architecture", Title: "Verified boundary", Content: "The core remains provider neutral.", Rationale: "Durable design rule", Sensitivity: sensitivity, Evidence: []handoff.Reference{evidence}}
				if uncertain {
					p.Uncertainty = []string{"Confirm intended scope."}
				}
				out.MemoryProposals = []handoff.MemoryProposal{p}
			}
			return out, nil
		}), nil
	}
	return cwd, s
}

func memoryCommand(t *testing.T, cwd string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := Run(args, cwd, &out, &err)
	return code, out.String(), err.String()
}

func TestAutomaticMemoryAndRecall(t *testing.T) {
	cwd, s := memoryFixture(t, "nonsensitive", false)
	s.recall = true
	code, out, err := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal", "--policy", "planned")
	if code != 0 || err != "" || !strings.Contains(out, "status: completed") || !strings.Contains(out, "memory[0]: saved") || s.creates != 1 {
		t.Fatalf("%d %s %s creates=%d", code, out, err, s.creates)
	}
	code, out, err = memoryCommand(t, cwd, "memory", "status", "--workflow", "demo")
	if code != 0 || err != "" || !strings.Contains(out, "memory[0]: saved") || s.creates != 1 {
		t.Fatalf("status %d %s %s", code, out, err)
	}
}

func TestExactMemoryDecisions(t *testing.T) {
	for _, answer := range []string{"grant", "decline"} {
		t.Run(answer, func(t *testing.T) {
			cwd, s := memoryFixture(t, "nonsensitive", true)
			code, out, err := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
			if code != 0 || err != "" || !strings.Contains(out, "memory[0]: pending") || s.creates != 0 {
				t.Fatalf("%d %s %s", code, out, err)
			}
			plan := ""
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "plan[0]: ") {
					plan = strings.TrimPrefix(line, "plan[0]: ")
				}
			}
			if plan == "" {
				t.Fatal("missing decision identity")
			}
			if code, _, _ := memoryCommand(t, cwd, "memory", answer, "--workflow", "demo", "--proposal", "0", "--plan", "sha256:"+strings.Repeat("0", 64)); code == 0 {
				t.Fatal("stale decision accepted")
			}
			if s.creates != 0 {
				t.Fatal("stale decision saved")
			}
			code, out, err = memoryCommand(t, cwd, "memory", answer, "--workflow", "demo", "--proposal", "0", "--plan", plan)
			want := "declined"
			calls := 0
			if answer == "grant" {
				want = "saved"
				calls = 1
			}
			if code != 0 || err != "" || !strings.Contains(out, "memory[0]: "+want) || s.creates != calls {
				t.Fatalf("decision %d %s %s", code, out, err)
			}
			code, out, err = memoryCommand(t, cwd, "memory", "status", "--workflow", "demo")
			if code != 0 || err != "" || !strings.Contains(out, "memory[0]: "+want) || s.creates != calls {
				t.Fatalf("status %d %s %s", code, out, err)
			}
		})
	}
}

func TestMemoryPrivacyAndUnavailable(t *testing.T) {
	for _, mode := range []string{"sensitive", "unknown", "none", "offline"} {
		t.Run(mode, func(t *testing.T) {
			privacy := mode
			if mode == "offline" {
				privacy = "none"
			}
			cwd, s := memoryFixture(t, privacy, false)
			s.fail = mode == "offline"
			code, out, err := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
			if code != 0 || !strings.Contains(out, "status: completed") || s.creates != 0 {
				t.Fatalf("%d %s %s", code, out, err)
			}
			if mode == "offline" && !strings.Contains(err, "memory unavailable") {
				t.Fatal("missing memory warning")
			}
			if mode == "sensitive" || mode == "unknown" {
				if !strings.Contains(out, "memory[0]: skipped") {
					t.Fatal(out)
				}
			}
			if _, err := os.Stat(filepath.Join(cwd, ".koard", "state", "runs", "demo", "memory")); !os.IsNotExist(err) {
				t.Fatalf("unretained proposal journaled: %v", err)
			}
		})
	}
}

func TestMemoryCommandParsing(t *testing.T) {
	for _, args := range [][]string{
		{"status"}, {"grant", "--workflow", "demo", "--proposal", "0"},
		{"status", "--workflow", "demo", "--workflow", "other"},
		{"status", "--workflow", "../escape"}, {"status", "--workflow", "demo", "--proposal", "-1"},
		{"status", "--workflow", "demo", "--plan", "unused"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cwd, _ := memoryFixture(t, "none", false)
			if code, _, _ := memoryCommand(t, cwd, append([]string{"memory"}, args...)...); code == 0 {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
}

func TestMemoryPromptMatchesProtocolLimits(t *testing.T) {
	for _, text := range []string{"title (1-256 bytes)", "content (1-4096 bytes)", "rationale (1-1024 bytes)", "512 bytes each"} {
		if !strings.Contains(memoryInstructions, text) {
			t.Errorf("prompt missing protocol limit %q", text)
		}
	}
}

func TestUnknownMemoryWriteAndTamperedWorkflow(t *testing.T) {
	cwd, s := memoryFixture(t, "nonsensitive", false)
	s.lost = true
	code, out, errOut := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
	if code != 0 || !strings.Contains(out, "memory[0]: unknown") || errOut == "" || s.creates != 1 {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	code, out, errOut = memoryCommand(t, cwd, "memory", "status", "--workflow", "demo")
	if code != 0 || errOut != "" || !strings.Contains(out, "memory[0]: unknown") || s.creates != 1 {
		t.Fatalf("status %d %s %s", code, out, errOut)
	}
	path := filepath.Join(cwd, ".koard", "state", "runs", "demo", "handoffs", "1-result.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := memoryCommand(t, cwd, "memory", "status", "--workflow", "demo"); code == 0 {
		t.Fatal("tampered persisted evidence accepted")
	}
	if s.creates != 1 {
		t.Fatal("tamper caused another write")
	}
}

func TestMemoryStatusDoesNotCreatePendingJournal(t *testing.T) {
	cwd, s := memoryFixture(t, "nonsensitive", true)
	if code, _, _ := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal"); code != 0 {
		t.Fatal(code)
	}
	dir := filepath.Join(cwd, ".koard", "state", "runs", "demo", "memory")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if code, out, err := memoryCommand(t, cwd, "memory", "status", "--workflow", "demo"); code != 0 || !strings.Contains(out, "pending") || err != "" {
		t.Fatalf("%d %s %s", code, out, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("status created journal")
	}
	if s.creates != 0 {
		t.Fatal("status saved knowledge")
	}
}

func TestProviderReferenceTranslationRoundTrip(t *testing.T) {
	ref := handoff.Reference{Kind: agentrouter.ResultReferenceKind, URI: "handoffs/1-result.json", Digest: "sha256:" + strings.Repeat("a", 64)}
	r := handoff.Request{SchemaVersion: 1, ID: "verify", TaskID: "task", From: handoff.RoleExecutor, To: handoff.RoleVerifier, Objective: "Verify", ExpectedOutput: handoff.OutputContract{Format: "application/json"}, Context: []handoff.Reference{{Kind: "memory", URI: "repo://advisory", Digest: ref.Digest}, ref}}
	runner := workflowRunner{runPath: ".koard/state/runs/demo", roles: map[handoff.Role]agentrouter.Runner{handoff.RoleVerifier: workflowFake(func(_ context.Context, visible handoff.Request) (handoff.Result, error) {
		if visible.Context[1].URI != "repo://.koard/state/runs/demo/handoffs/1-result.json" {
			t.Fatal("nonzero reference not translated")
		}
		return handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Verified", MemoryProposals: []handoff.MemoryProposal{{Type: "discovery", Title: "Finding", Content: "Supported finding", Rationale: "Durable", Sensitivity: "nonsensitive", Evidence: []handoff.Reference{visible.Context[1]}}}}, nil
	})}}
	result, err := runner.Execute(context.Background(), r)
	if err != nil || result.MemoryProposals[0].Evidence[0] != ref {
		t.Fatalf("round trip %+v %v", result, err)
	}
	if err := handoff.ValidateResultFor(r, result); err != nil {
		t.Fatal(err)
	}
	if r.Context[1] != ref {
		t.Fatal("mutated original request")
	}
}
