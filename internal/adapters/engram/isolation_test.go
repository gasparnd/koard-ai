package engram

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/memory"
)

func verifierHash(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// S2/S5: real owned storage, project isolation, authentication, rejection invariants.
func TestVerifierRealIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("real Engram 3.7.2")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("ENGRAM_URL", "http://127.0.0.1:1")
	t.Setenv("ENGRAM_CLOUD_AUTOSYNC", "1")
	t.Setenv("GIT_DIR", "/nonexistent")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data := filepath.Join(root, "data")
	var first memory.Project
	for i := 0; i < 2; i++ {
		workspace := filepath.Join(root, fmt.Sprintf("project-%d", i))
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		a, err := Start(ctx, Options{Executable: verifierBinary(t), Workspace: workspace, DataDir: data})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Close(context.Background()) })
		p, err := a.Resolve(ctx, workspace)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p
		} else if first.Name == p.Name {
			t.Fatal("projects collapsed")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", a.endpoint+"/search?q=verifier&project="+p.Name+"&scope=project", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := a.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("unauthenticated status %d", resp.StatusCode)
		}
		before := verifierHash(t, data)
		if _, err := a.Create(ctx, "foreign", "op", handoff.MemoryProposal{}); err == nil || err.Error() != "Engram session is closed or project does not match" {
			t.Fatalf("foreign create: %v", err)
		}
		for _, q := range []string{"", "  ", strings.Repeat("x", 4097)} {
			if _, err := a.Search(ctx, p.Name, q); err == nil || err.Error() != "Engram search requires a bounded nonempty query" {
				t.Fatalf("query rejection: %v", err)
			}
		}
		if _, err := a.Resolve(ctx, root); err == nil || err.Error() != "Engram workspace does not match registered session" {
			t.Fatalf("workspace rejection: %v", err)
		}
		if !reflect.DeepEqual(before, verifierHash(t, data)) {
			t.Fatal("rejected operations mutated storage")
		}
		hits, err := a.Search(ctx, p.Name, "verifierunique")
		if err != nil || len(hits) != 0 {
			t.Fatalf("cross-project leak: %+v %v", hits, err)
		}
		proposal := handoff.MemoryProposal{Type: "discovery", Title: "verifierunique", Content: fmt.Sprintf("Durable isolated knowledge for project %d.", i)}
		saved, err := a.Create(ctx, p.Name, fmt.Sprintf("verifier-%d", i), proposal)
		if err != nil || saved.Project != p.Name || saved.Content != proposal.Content {
			t.Fatalf("save: %+v %v", saved, err)
		}
		hits, err = a.Search(ctx, p.Name, "verifierunique")
		if err != nil || len(hits) != 1 || hits[0].ID != saved.ID {
			t.Fatalf("recall %+v %v", hits, err)
		}
		var wg sync.WaitGroup
		for j := 0; j < 12; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				if j%3 == 0 {
					if err := a.Close(ctx); err != nil {
						t.Errorf("close: %v", err)
					}
					return
				}
				_, err := a.Search(ctx, p.Name, "verifierunique")
				if err != nil && err.Error() != "Engram session is closed or project does not match" {
					t.Errorf("concurrent search: %v", err)
				}
			}(j)
		}
		wg.Wait()
		select {
		case <-a.done:
		default:
			t.Fatal("child survived Close")
		}
	}
}

// S2/S5: startup failure must not replace an explicit invalid storage file.
func TestVerifierStorageStartupFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("real Engram 3.7.2")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	data := filepath.Join(root, "data")
	if err := os.WriteFile(data, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	before := verifierHash(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, err := Start(ctx, Options{Executable: verifierBinary(t), Workspace: root, DataDir: data})
	if err == nil {
		a.Close(ctx)
		t.Fatal("invalid storage accepted")
	}
	if err.Error() != "invalid Engram readiness frame" {
		t.Fatalf("startup error: %v", err)
	}
	if !reflect.DeepEqual(before, verifierHash(t, root)) {
		t.Fatal("failed startup changed files")
	}
}

// S1/S2: acknowledged writes with a lost full read are uncertain and never retried.
func TestVerifierUncertainSave(t *testing.T) {
	posts, gets := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer verifier-token" {
			t.Error("missing owned token")
		}
		if r.Method == "POST" {
			posts++
			w.WriteHeader(201)
			fmt.Fprint(w, `{"id":9,"status":"saved"}`)
			return
		}
		gets++
		w.WriteHeader(503)
	}))
	defer s.Close()
	a := &Adapter{client: s.Client(), endpoint: s.URL, token: "verifier-token", project: memory.Project{Name: "project"}, session: "session", done: make(chan struct{})}
	_, err := a.Create(context.Background(), "project", "operation", handoff.MemoryProposal{Type: "discovery", Title: "Title", Content: "Content"})
	if err == nil || err.Error() != "Engram returned HTTP 503" {
		t.Fatalf("uncertain error: %v", err)
	}
	if posts != 1 || gets != 1 {
		t.Fatalf("requests post=%d get=%d", posts, gets)
	}
}

func verifierBinary(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("engram")
	if err != nil {
		t.Skip("Engram unavailable")
	}
	out, err := exec.Command(binary, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "engram 3.7.2" {
		t.Skip("Engram 3.7.2 required")
	}
	return binary
}

// S2: foreign full records cannot enter recall, even after an owned search hit.
func TestVerifierForeignFullRecord(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("project") != "ours" || r.URL.Query().Get("scope") != "project" || r.URL.Query().Get("limit") != "20" {
				t.Error("unscoped query")
			}
			fmt.Fprint(w, `[{"id":1,"project":"ours","scope":"project"}]`)
		case "/observations/1":
			fmt.Fprint(w, `{"id":1,"project":"theirs","scope":"project","title":"Private","content":"Foreign knowledge"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	a := &Adapter{client: s.Client(), endpoint: s.URL, project: memory.Project{Name: "ours"}, done: make(chan struct{})}
	hits, err := a.Search(context.Background(), "ours", "knowledge")
	if err == nil || err.Error() != "Engram full observation ownership or payload mismatch" || len(hits) != 0 {
		t.Fatalf("foreign content accepted: %+v %v", hits, err)
	}
}
