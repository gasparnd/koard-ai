package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/config"
	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/memory"
)

func verifierTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if i.Mode().IsRegular() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			fmt.Fprintf(h, "%s:%x\n", strings.TrimPrefix(p, root), sha256.Sum256(b))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
func verifierPlan(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "plan[0]: ") {
			return strings.TrimPrefix(l, "plan[0]: ")
		}
	}
	return ""
}

func TestVerifierDecisionInvariants(t *testing.T) {
	for _, action := range []string{"grant", "decline", "unknown"} {
		t.Run(action, func(t *testing.T) {
			cwd, s := memoryFixture(t, "nonsensitive", true)
			s.recall = true
			code, out, err := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal", "--policy", "planned")
			if code != 0 || err != "" || !strings.Contains(out, "memory[0]: pending") {
				t.Fatalf("run %d %s %s", code, out, err)
			}
			plan := verifierPlan(out)
			if plan == "" {
				t.Fatal("no plan")
			}
			for _, args := range [][]string{
				{"status", "--workflow", "../escape"}, {"status", "--workflow", "demo", "--proposal", "1"},
				{"grant", "--workflow", "demo", "--proposal", "0", "--plan", "sha256:" + strings.Repeat("0", 64)},
				{"decline", "--workflow", "demo", "--proposal", "0", "--plan", plan, "--plan", plan},
			} {
				before := verifierTree(t, cwd)
				c, o, e := memoryCommand(t, cwd, append([]string{"memory"}, args...)...)
				if c == 0 {
					t.Fatalf("accepted %v: %s %s", args, o, e)
				}
				if before != verifierTree(t, cwd) || s.creates != 0 {
					t.Fatal("rejection mutated state")
				}
			}
			answer := action
			if action == "unknown" {
				answer = "grant"
				s.lost = true
			}
			c, o, e := memoryCommand(t, cwd, "memory", answer, "--workflow", "demo", "--proposal", "0", "--plan", plan)
			want := "saved"
			if action == "decline" {
				want = "declined"
			}
			if action == "unknown" {
				want = "unknown"
			}
			if !strings.Contains(o, "memory[0]: "+want) || (action != "unknown" && (c != 0 || e != "")) {
				t.Fatalf("decision %d %s %s", c, o, e)
			}
			before := verifierTree(t, cwd)
			calls := s.creates
			c, o, e = memoryCommand(t, cwd, "memory", "status", "--workflow", "demo", "--proposal", "0")
			if c != 0 || e != "" || !strings.Contains(o, "memory[0]: "+want) || before != verifierTree(t, cwd) {
				t.Fatalf("status %d %s %s", c, o, e)
			}
			memoryCommand(t, cwd, "memory", "grant", "--workflow", "demo", "--proposal", "0", "--plan", plan)
			if s.creates != calls {
				t.Fatal("retried settled operation")
			}
		})
	}
}

func TestVerifierEvidenceRefusals(t *testing.T) {
	for _, mode := range []string{"missing", "changed", "escape", "recall", "recall-alias", "recall-hardlink"} {
		t.Run(mode, func(t *testing.T) {
			cwd, s := memoryFixture(t, "none", false)
			body := []byte("Untrusted advisory data")
			target := filepath.Join(cwd, ".koard", "state", "memory", "recall", "probe.json")
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, body, 0600); err != nil {
				t.Fatal(err)
			}
			ref := handoff.Reference{Kind: "artifact", URI: "repo://source.txt", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body))}
			var err error
			switch mode {
			case "changed":
				err = os.WriteFile(filepath.Join(cwd, "source.txt"), []byte("different"), 0600)
			case "escape":
				outside := filepath.Join(t.TempDir(), "outside")
				if err = os.WriteFile(outside, body, 0600); err == nil {
					err = os.Symlink(outside, filepath.Join(cwd, "source.txt"))
				}
			case "recall":
				ref.URI = "repo://.koard/state/memory/recall/probe.json"
			case "recall-alias":
				err = os.Symlink(target, filepath.Join(cwd, "source.txt"))
			case "recall-hardlink":
				err = os.Link(target, filepath.Join(cwd, "source.txt"))
			}
			if err != nil {
				t.Fatal(err)
			}
			r := handoff.Request{SchemaVersion: 1, ID: "verify", TaskID: "task", From: handoff.RoleExecutor, To: handoff.RoleVerifier, Objective: "Verify", ExpectedOutput: handoff.OutputContract{Format: "application/json"}, Context: []handoff.Reference{ref}}
			result := handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Verified", MemoryProposals: []handoff.MemoryProposal{{Type: "discovery", Title: "Boundary", Content: "Durable assertion", Rationale: "Useful later", Sensitivity: "nonsensitive", Evidence: []handoff.Reference{ref}}}}
			before := verifierTree(t, cwd)
			_, err = memoryCoordinator(cwd, "demo", s).Execute(context.Background(), memory.Input{Request: r, Result: result}, nil)
			if err == nil || s.creates != 0 {
				t.Fatalf("evidence accepted: err=%v creates=%d", err, s.creates)
			}
			if before != verifierTree(t, cwd) {
				t.Fatal("rejected evidence mutated state")
			}
		})
	}
}

func TestVerifierPublicRecallAlias(t *testing.T) {
	cwd, s := memoryFixture(t, "none", false)
	body := []byte("Untrusted advisory data")
	target := filepath.Join(cwd, ".koard", "state", "memory", "recall", "probe.json")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(cwd, "source.txt")); err != nil {
		t.Fatal(err)
	}
	ref := handoff.Reference{Kind: "artifact", URI: "repo://source.txt", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body))}
	workflowProvider = func(string, config.AgentConfig) (agentrouter.Runner, error) {
		return workflowFake(func(_ context.Context, r handoff.Request) (handoff.Result, error) {
			out := handoff.Result{SchemaVersion: 1, HandoffID: r.ID, Role: r.To, Status: handoff.StatusCompleted, Summary: "Completed"}
			if r.To == handoff.RoleExecutor {
				out.Artifacts = []handoff.Reference{ref}
			} else {
				out.MemoryProposals = []handoff.MemoryProposal{{Type: "discovery", Title: "Recall assertion", Content: "Durable assertion", Rationale: "Useful", Sensitivity: "nonsensitive", Evidence: []handoff.Reference{ref}}}
			}
			return out, nil
		}), nil
	}
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
	if c != 0 {
		t.Fatalf("workflow %d %s %s", c, o, e)
	}
	if s.creates != 0 {
		t.Fatalf("recall alias saved through public run: %s %s", o, e)
	}
}

type verifierForeignStore struct{ *cliMemory }

func TestVerifierRecallIdentityRecheck(t *testing.T) {
	for _, mode := range []string{"recall-symlink", "recall-hardlink", "source", "source-symlink", "source-hardlink", "independent-equal-bytes"} {
		t.Run(mode, func(t *testing.T) {
			cwd, s := memoryFixture(t, "none", false)
			body := []byte("Evidence bytes")
			recall := filepath.Join(cwd, ".koard", "state", "memory", "recall")
			if err := os.MkdirAll(recall, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(recall, "probe.json")
			if err := os.WriteFile(target, body, 0600); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(cwd, "source.txt")
			alias := filepath.Join(cwd, "alias.txt")
			if err := os.WriteFile(source, body, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "recall-symlink":
				err = os.Symlink(target, alias)
			case "recall-hardlink":
				err = os.Link(target, alias)
			case "source-symlink":
				err = os.Symlink(source, alias)
			case "source-hardlink":
				err = os.Link(source, alias)
			default:
				err = os.WriteFile(alias, body, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := verifierTree(t, cwd)
			ref := handoff.Reference{Kind: "artifact", URI: "repo://alias.txt", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body))}
			got, err := memoryCoordinator(cwd, "demo", s).ReadEvidence(context.Background(), ref)
			if strings.HasPrefix(mode, "recall-") {
				if err == nil || err.Error() != "advisory recall is not source evidence" || got != nil {
					t.Fatalf("alias admitted: %q %v", got, err)
				}
			} else if err != nil || string(got) != string(body) {
				t.Fatalf("valid source refused: %q %v", got, err)
			}
			if before != verifierTree(t, cwd) || s.creates != 0 {
				t.Fatal("evidence read mutated state")
			}
		})
	}
}

func TestVerifierLegitimateRetentionRecheck(t *testing.T) {
	cwd, s := memoryFixture(t, "nonsensitive", false)
	s.recall = true
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "valid", "--task", "task", "--objective", "Goal")
	if c != 0 || e != "" || !strings.Contains(o, "memory[0]: saved") || s.creates != 1 {
		t.Fatalf("legitimate retention: %d %s %s creates=%d", c, o, e, s.creates)
	}
}

func (s verifierForeignStore) Search(context.Context, string, string) ([]memory.Observation, error) {
	return []memory.Observation{{ID: 1, Project: "foreign", Scope: "project", State: "active", Title: "secret", Content: "foreign secret"}}, nil
}
func TestVerifierOwnershipAndPrompt(t *testing.T) {
	cwd, s := memoryFixture(t, "none", false)
	memoryProvider = func(context.Context, string) (memoryClient, error) { return verifierForeignStore{s}, nil }
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "foreign", "--task", "task", "--objective", "Goal")
	if c != 0 || !strings.Contains(e, "ownership mismatch") || strings.Contains(o, "foreign secret") || s.creates != 0 {
		t.Fatalf("%d %s %s", c, o, e)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".koard", "state", "memory", "recall")); !os.IsNotExist(err) {
		t.Fatal("foreign data materialized")
	}
}

func TestVerifierRealEngramCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("isolated real Engram")
	}
	binary, err := exec.LookPath("engram")
	if err != nil {
		t.Skip("Engram unavailable")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "engram 3.7.2" {
		t.Skip("Engram 3.7.2 required")
	}
	realProvider := memoryProvider
	cwd, _ := memoryFixture(t, "nonsensitive", true)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ENGRAM_DATA_DIR", t.TempDir())
	memoryProvider = realProvider
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "real", "--task", "task", "--objective", "Goal")
	if c != 0 || e != "" || !strings.Contains(o, "memory[0]: pending") {
		t.Fatalf("run %d %s %s", c, o, e)
	}
	plan := verifierPlan(o)
	c, o, e = memoryCommand(t, cwd, "memory", "grant", "--workflow", "real", "--proposal", "0", "--plan", plan)
	if c != 0 || e != "" || !strings.Contains(o, "memory[0]: saved") {
		t.Fatalf("grant %d %s %s", c, o, e)
	}
	c, o, e = memoryCommand(t, cwd, "memory", "status", "--workflow", "real")
	if c != 0 || e != "" || !strings.Contains(o, "memory[0]: saved") {
		t.Fatalf("status %d %s %s", c, o, e)
	}
	c, o, e = memoryCommand(t, cwd, "run", "--workflow", "recall", "--task", "task", "--objective", "Verified boundary")
	if c != 0 || e != "" {
		t.Fatalf("recall %d %s %s", c, o, e)
	}
	for _, name := range []string{"1-request.json", "2-request.json"} {
		b, err := os.ReadFile(filepath.Join(cwd, ".koard", "state", "runs", "recall", "handoffs", name))
		if err != nil {
			t.Fatal(err)
		}
		r, err := handoff.DecodeRequest(strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ref := range r.Context {
			if ref.Kind == "memory" {
				found = true
			}
		}
		if !found || !strings.Contains(r.Objective, "untrusted advisory") {
			t.Fatalf("missing persisted recall notice: %s", b)
		}
		if strings.Contains(r.Objective, "memory_proposals") != (r.To == handoff.RoleVerifier) {
			t.Fatal("wrong proposal instruction role")
		}
	}
}

func TestVerifierPersistedEvidenceTamper(t *testing.T) {
	for _, name := range []string{"1-request.json", "1-result.json", "2-request.json", "2-result.json"} {
		t.Run(name, func(t *testing.T) {
			cwd, s := memoryFixture(t, "nonsensitive", true)
			c, o, e := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
			if c != 0 || e != "" {
				t.Fatalf("%d %s %s", c, o, e)
			}
			p := filepath.Join(cwd, ".koard", "state", "runs", "demo", "handoffs", name)
			if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			before := verifierTree(t, cwd)
			c, o, e = memoryCommand(t, cwd, "memory", "grant", "--workflow", "demo", "--proposal", "0", "--plan", verifierPlan(o))
			if c != 1 || o != "" || e != "koard memory: workflow evidence changed: reference digest does not match repository bytes\n" {
				t.Fatalf("%d %q %q", c, o, e)
			}
			if before != verifierTree(t, cwd) || s.creates != 0 {
				t.Fatal("tamper rejection mutated state")
			}
		})
	}
}

type verifierDuplicateStore struct{ *cliMemory }

func (s verifierDuplicateStore) Search(_ context.Context, p, q string) ([]memory.Observation, error) {
	if q == "Goal" {
		return nil, nil
	}
	return []memory.Observation{{ID: 7, Project: p, Scope: "project", State: "active", Type: "architecture", Title: "Verified boundary", Content: "The core remains provider neutral."}}, nil
}
func TestVerifierDuplicateDecision(t *testing.T) {
	cwd, s := memoryFixture(t, "nonsensitive", false)
	memoryProvider = func(context.Context, string) (memoryClient, error) { return verifierDuplicateStore{s}, nil }
	c, o, e := memoryCommand(t, cwd, "run", "--workflow", "demo", "--task", "task", "--objective", "Goal")
	if c != 0 || e != "" || !strings.Contains(o, "memory[0]: duplicate") {
		t.Fatalf("%d %s %s", c, o, e)
	}
	plan := verifierPlan(o)
	for _, answer := range []string{"grant", "decline"} {
		before := verifierTree(t, cwd)
		c, o, e = memoryCommand(t, cwd, "memory", answer, "--workflow", "demo", "--proposal", "0", "--plan", plan)
		if c != 1 || e == "" || s.creates != 0 || before != verifierTree(t, cwd) {
			t.Fatalf("%s %d %s %s", answer, c, o, e)
		}
	}
}
