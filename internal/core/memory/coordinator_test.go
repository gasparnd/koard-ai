package memory_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/memory"
)

type fakeStore struct {
	mu        sync.Mutex
	project   memory.Project
	related   []memory.Observation
	err       error
	createErr error
	foreign   bool
	calls     int
	queries   []string
}

func (s *fakeStore) Resolve(ctx context.Context, cwd string) (memory.Project, error) {
	return s.project, s.err
}

func (s *fakeStore) Search(ctx context.Context, project, query string) ([]memory.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, project)
	return append([]memory.Observation(nil), s.related...), s.err
}

func (s *fakeStore) Create(ctx context.Context, project, operation string, p handoff.MemoryProposal) (memory.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if operation == "" {
		return memory.Observation{}, errors.New("missing operation identity")
	}
	if s.foreign {
		project = "other"
	}
	return memory.Observation{ID: 1, Project: project, Scope: "project", Type: p.Type, Title: p.Title, Content: p.Content, State: "active"}, s.createErr
}

func fixture(t *testing.T) (*memory.Coordinator, *fakeStore, memory.Input) {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeStore{project: memory.Project{Name: "canonical-project", Directory: workspace}}
	body := []byte("Architecture declaration inspected by verifier.")
	ref := handoff.Reference{Kind: "source", URI: "repo://architecture.md", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body))}
	p := handoff.MemoryProposal{Type: "architecture", Title: "Core boundary", Content: "Core must not import provider adapters.", Rationale: "Retain the repository dependency rule.", Evidence: []handoff.Reference{ref}, Sensitivity: "nonsensitive"}
	in := memory.Input{
		Request: handoff.Request{SchemaVersion: 1, ID: "verify", TaskID: "task", From: handoff.RoleExecutor, To: handoff.RoleVerifier, Objective: "Verify", Context: []handoff.Reference{ref}, ExpectedOutput: handoff.OutputContract{Format: "json"}},
		Result:  handoff.Result{SchemaVersion: 1, HandoffID: "verify", Role: handoff.RoleVerifier, Status: handoff.StatusCompleted, Summary: "Checked", MemoryProposals: []handoff.MemoryProposal{p}},
	}
	c := &memory.Coordinator{Workspace: workspace, JournalDir: filepath.Join(workspace, "journal"), Store: s, ReadEvidence: func(ctx context.Context, ref handoff.Reference) ([]byte, error) { return body, nil }}
	return c, s, in
}

func TestAssessmentPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		change       func(*fakeStore, *memory.Input)
	}{
		{"supported", "automatic", func(s *fakeStore, in *memory.Input) {}},
		{"uncertain", "pending", func(s *fakeStore, in *memory.Input) {
			in.Result.MemoryProposals[0].Uncertainty = []string{"The scope may be narrower."}
		}},
		{"sensitive", "skipped", func(s *fakeStore, in *memory.Input) { in.Result.MemoryProposals[0].Sensitivity = "sensitive" }},
		{"unknown privacy", "skipped", func(s *fakeStore, in *memory.Input) { in.Result.MemoryProposals[0].Sensitivity = "unknown" }},
		{"related difference", "pending", func(s *fakeStore, in *memory.Input) {
			s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "project", Type: "architecture", Title: "Core boundary", Content: "Different rule", State: "active"}}
		}},
		{"duplicate", "duplicate", func(s *fakeStore, in *memory.Input) {
			p := in.Result.MemoryProposals[0]
			s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "project", Type: p.Type, Title: p.Title, Content: p.Content, State: "active"}}
		}},
		{"stale memory", "pending", func(s *fakeStore, in *memory.Input) {
			p := in.Result.MemoryProposals[0]
			s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "project", Type: p.Type, Title: p.Title, Content: p.Content, State: "needs_review"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s, in := fixture(t)
			tc.change(s, &in)
			p, err := c.Assess(context.Background(), in)
			if err != nil || p.Action != tc.status {
				t.Fatalf("plan %+v error %v", p, err)
			}
			if s.calls != 0 {
				t.Fatal("assessment wrote to store")
			}
			if _, err := os.Stat(c.JournalDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("assessment created journal: %v", err)
			}
		})
	}
}

func TestEvidenceAndProjectRefusals(t *testing.T) {
	for _, name := range []string{"empty project", "wrong directory", "foreign result", "wrong scope", "wrong evidence", "missing evidence", "unbound evidence", "missing store", "bad index", "failed result"} {
		t.Run(name, func(t *testing.T) {
			c, s, in := fixture(t)
			switch name {
			case "empty project":
				s.project.Name = ""
			case "wrong directory":
				s.project.Directory = filepath.Dir(c.Workspace)
			case "foreign result":
				s.related = []memory.Observation{{ID: 2, Project: "other", Scope: "project"}}
			case "wrong scope":
				s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "global"}}
			case "wrong evidence":
				c.ReadEvidence = func(context.Context, handoff.Reference) ([]byte, error) { return []byte("changed"), nil }
			case "missing evidence":
				c.ReadEvidence = func(context.Context, handoff.Reference) ([]byte, error) { return nil, os.ErrNotExist }
			case "unbound evidence":
				in.Request.Context = nil
			case "missing store":
				c.Store = nil
			case "bad index":
				in.ProposalIndex = 1
			case "failed result":
				in.Result.Status = handoff.StatusFailed
			}
			if _, err := c.Execute(context.Background(), in, nil); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if s.calls != 0 {
				t.Fatal("unsafe input wrote")
			}
		})
	}
}

func TestAutomaticSaveAndReplay(t *testing.T) {
	c, s, in := fixture(t)
	for i := 0; i < 2; i++ {
		out, err := c.Execute(context.Background(), in, nil)
		if err != nil || out.Status != "saved" || out.ObservationID != 1 {
			t.Fatalf("out %+v err %v", out, err)
		}
	}
	if s.calls != 1 {
		t.Fatalf("created %d times", s.calls)
	}
	for _, project := range s.queries {
		if project != "canonical-project" {
			t.Fatal("wrong query scope")
		}
	}
}

func TestExactDecision(t *testing.T) {
	for _, choice := range []string{"granted", "declined"} {
		t.Run(choice, func(t *testing.T) {
			c, s, in := fixture(t)
			in.Result.MemoryProposals[0].Uncertainty = []string{"Confirm durability."}
			out, err := c.Execute(context.Background(), in, nil)
			if err != nil || out.Status != "pending" || s.calls != 0 {
				t.Fatalf("%+v %v", out, err)
			}
			decision := &memory.Decision{PlanID: out.PlanID, Answer: choice}
			out, err = c.Execute(context.Background(), in, decision)
			want := "declined"
			calls := 0
			if choice == "granted" {
				want = "saved"
				calls = 1
			}
			if err != nil || out.Status != want || s.calls != calls {
				t.Fatalf("%+v %v calls=%d", out, err, s.calls)
			}
			decision.Answer = map[string]string{"granted": "declined", "declined": "granted"}[choice]
			if _, err = c.Execute(context.Background(), in, decision); err == nil {
				t.Fatal("conflicting decision accepted")
			}
		})
	}
}

func TestStaleDecisionAndUnavailableSearch(t *testing.T) {
	c, s, in := fixture(t)
	in.Result.MemoryProposals[0].Uncertainty = []string{"Check."}
	out, err := c.Execute(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &memory.Decision{PlanID: out.PlanID, Answer: "granted"}
	in.Result.MemoryProposals[0].Content = "Changed claim"
	if _, err = c.Execute(context.Background(), in, d); err == nil {
		t.Fatal("stale grant accepted")
	}
	s.err = errors.New("offline")
	if _, err = c.Execute(context.Background(), in, nil); err == nil {
		t.Fatal("offline lookup treated as absence")
	}
	if s.calls != 0 {
		t.Fatal("unsafe write")
	}
}

func TestChangedSearchCannotForgetDecline(t *testing.T) {
	c, s, in := fixture(t)
	s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "project", Content: "Different rule", State: "active"}}
	out, err := c.Execute(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Execute(context.Background(), in, &memory.Decision{PlanID: out.PlanID, Answer: "declined"}); err != nil {
		t.Fatal(err)
	}
	s.related = nil
	if _, err = c.Execute(context.Background(), in, nil); err == nil {
		t.Fatal("changed search silently forgot the decline")
	}
	if s.calls != 0 {
		t.Fatal("declined proposal was saved")
	}
}

func TestUnknownWriteNeverRetries(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			c, s, in := fixture(t)
			s.foreign = foreign
			if !foreign {
				s.createErr = errors.New("lost response")
			}
			for i := 0; i < 2; i++ {
				out, err := c.Execute(context.Background(), in, nil)
				if err == nil || out.Status != "unknown" {
					t.Fatalf("%+v %v", out, err)
				}
			}
			if s.calls != 1 {
				t.Fatalf("retried unknown write: %d", s.calls)
			}
		})
	}
}

func TestUnknownAttemptSurvivesChangedSearchResults(t *testing.T) {
	c, s, in := fixture(t)
	s.related = []memory.Observation{{ID: 2, Project: s.project.Name, Scope: "project", Type: "architecture", Title: "Core boundary", Content: "Different rule", State: "active"}}
	s.createErr = errors.New("lost response")
	out, err := c.Execute(context.Background(), in, nil)
	if err != nil || out.Status != "pending" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = c.Execute(context.Background(), in, &memory.Decision{PlanID: out.PlanID, Answer: "granted"}); err == nil {
		t.Fatal("lost response was not reported")
	}
	// Another client removing a search hit changes the decision target, but must
	// not reset the already-spent operation identity of the same proposal.
	s.related = nil
	out, err = c.Execute(context.Background(), in, nil)
	if err == nil || out.Status != "unknown" || s.calls != 1 {
		t.Fatalf("unknown write retried: %+v %v calls=%d", out, err, s.calls)
	}
}

func TestConcurrentAttempts(t *testing.T) {
	c, s, in := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.Execute(context.Background(), in, nil) }()
	}
	wg.Wait()
	if s.calls != 1 {
		t.Fatalf("got %d creates", s.calls)
	}
}

func TestSensitiveProposalLeavesNoJournal(t *testing.T) {
	c, s, in := fixture(t)
	in.Result.MemoryProposals[0].Sensitivity = "sensitive"
	out, err := c.Execute(context.Background(), in, nil)
	if err != nil || out.Status != "skipped" || s.calls != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = os.Stat(c.JournalDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("sensitive proposal persisted")
	}
}

func TestDecisionsForNonRetainedPlansAreExplicitlyRefused(t *testing.T) {
	for _, state := range []string{"duplicate", "skipped"} {
		for _, answer := range []string{"granted", "declined"} {
			t.Run(state+"/"+answer, func(t *testing.T) {
				c, s, in := fixture(t)
				p := in.Result.MemoryProposals[0]
				if state == "skipped" {
					in.Result.MemoryProposals[0].Sensitivity = "sensitive"
				} else {
					s.related = []memory.Observation{{ID: 4, Project: s.project.Name, Scope: "project", Type: p.Type, Title: p.Title, Content: p.Content, State: "active"}}
				}
				plan, err := c.Assess(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				out, err := c.Execute(context.Background(), in, &memory.Decision{PlanID: plan.ID, Answer: answer})
				if err == nil || out.Status != state {
					t.Fatalf("decision silently ignored: %+v %v", out, err)
				}
				if s.calls != 0 {
					t.Fatal("refused decision wrote to store")
				}
				if _, err := os.Stat(c.JournalDir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused decision created journal")
				}
			})
		}
	}
}

func TestDuplicateDeclineCannotBecomeAutomaticSave(t *testing.T) {
	c, s, in := fixture(t)
	p := in.Result.MemoryProposals[0]
	s.related = []memory.Observation{{ID: 4, Project: s.project.Name, Scope: "project", Type: p.Type, Title: p.Title, Content: p.Content, State: "active"}}
	plan, err := c.Assess(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Execute(context.Background(), in, &memory.Decision{PlanID: plan.ID, Answer: "declined"})
	if err != nil {
		return
	} // An explicit refusal does not acknowledge consent.
	if out.Status != "declined" {
		t.Errorf("explicit decline ignored: status=%s", out.Status)
	}
	s.related = nil
	out, err = c.Execute(context.Background(), in, nil)
	if s.calls != 0 {
		t.Fatalf("declined operation later saved: %+v %v", out, err)
	}
}

func TestJournalTamperAndSymlink(t *testing.T) {
	for _, kind := range []string{"tamper", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			c, s, in := fixture(t)
			in.Result.MemoryProposals[0].Uncertainty = []string{"Check."}
			out, err := c.Execute(context.Background(), in, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(c.JournalDir, strings.TrimPrefix(out.PlanID, "sha256:"), "plan.json")
			if err = os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if kind == "tamper" {
				err = os.WriteFile(path, []byte("{}"), 0600)
			} else {
				err = os.Symlink(filepath.Join(t.TempDir(), "outside"), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Execute(context.Background(), in, &memory.Decision{PlanID: out.PlanID, Answer: "granted"}); err == nil {
				t.Fatal("tampered journal accepted")
			}
			if s.calls != 0 {
				t.Fatal("unsafe write")
			}
		})
	}
}
