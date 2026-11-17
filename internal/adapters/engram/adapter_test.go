package engram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
)

const helperInstance = "0123456789abcdef0123456789abcdef"

// A separate fake child exercises the real stdin/stdout and HTTP boundary. It
// never invokes Engram or opens a database; all trace state is temporary.
func TestOwnedHelper(t *testing.T) {
	if os.Getenv("KOARD_ENGRAM_HELPER") != "1" {
		return
	}
	mode := os.Getenv("KOARD_ENGRAM_CASE")
	trace := os.Getenv("KOARD_ENGRAM_TRACE")
	log := func(s string) {
		f, _ := os.OpenFile(trace, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if f != nil {
			fmt.Fprintln(f, s)
			f.Close()
		}
	}
	for _, name := range []string{"ENGRAM_URL", "ENGRAM_PORT", "ENGRAM_SOCKET", "ENGRAM_PROJECT", "ENGRAM_CLOUD_TOKEN"} {
		if _, exists := os.LookupEnv(name); exists {
			os.Exit(3)
		}
	}
	if len(os.Getenv("ENGRAM_TOKEN")) != 64 || os.Getenv("ENGRAM_CLOUD_AUTOSYNC") != "0" || !filepath.IsAbs(os.Getenv("ENGRAM_DATA_DIR")) {
		os.Exit(4)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(5)
	}
	endpoint := "http://" + listener.Addr().String()
	var session, project string
	var stored map[string]any
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+os.Getenv("ENGRAM_TOKEN") {
			w.WriteHeader(401)
			return
		}
		log(r.Method + " " + r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/health":
			version := "3.7.2"
			if mode == "version" {
				version = "3.8.0"
			}
			instance := helperInstance
			if mode == "health" {
				instance = "other"
			}
			write(map[string]any{"status": "ok", "service": "engram", "version": version, "instance_id": instance, "capabilities": map[string]bool{"isolated_session_registration": true}})
		case "/compatibility":
			write(map[string]any{"revision": 1, "source": "active", "status": "ready", "active_reuse_authorized": mode != "compatibility", "instance_id": helperInstance, "runtime_id": "runtime", "capability": "startup-verified-store-v1"})
		case "/project/current":
			p := "canonical-project"
			source := "git_remote"
			var alternatives []string
			if mode == "ambiguous" {
				alternatives = []string{"one", "two"}
			}
			cwd := r.URL.Query().Get("cwd")
			if mode == "cwd" {
				cwd = "/other"
			}
			write(map[string]any{"project": p, "project_source": source, "cwd": cwd, "available_projects": alternatives})
		case "/sessions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["ownership_mode"] != "project_owned" || body["resume"] != false || body["directory"] == "" {
				w.WriteHeader(400)
				return
			}
			session, _ = body["id"].(string)
			project, _ = body["project"].(string)
			id := session
			if mode == "session" {
				id = "other"
			}
			w.WriteHeader(201)
			write(map[string]any{"id": id, "status": "created"})
		case "/search":
			q := r.URL.Query()
			if session == "" || q.Get("project") != project || q.Get("scope") != "project" || q.Get("limit") != "20" || q.Get("match_mode") != "all" || q.Has("all_projects") {
				w.WriteHeader(400)
				return
			}
			if mode == "redirect" {
				w.Header().Set("Location", endpoint+"/redirect-target")
				w.WriteHeader(302)
				return
			}
			if mode == "oversize" {
				fmt.Fprint(w, strings.Repeat(" ", (2<<20)+1))
				return
			}
			if mode == "search-foreign" {
				write([]any{map[string]any{"id": 1, "project": "other", "scope": "project"}})
				return
			}
			if stored == nil {
				write([]any{})
				return
			}
			write([]any{map[string]any{"id": 1, "project": project, "scope": "project", "content": "snippet"}})
		case "/observations":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["session_id"] != session || body["project"] != project || body["scope"] != "project" || body["capture_prompt"] != false || body["operation_id"] == "" {
				w.WriteHeader(400)
				return
			}
			for _, forbidden := range []string{"topic_key", "current_prompt"} {
				if _, exists := body[forbidden]; exists {
					w.WriteHeader(400)
					return
				}
			}
			stored = body
			stored["id"] = 1
			if mode == "lost" {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(201)
			write(map[string]any{"id": 1, "status": "saved"})
		case "/observations/1":
			if mode == "full-foreign" {
				stored["project"] = "other"
			}
			if mode == "full-id" {
				stored["id"] = 2
			}
			if mode == "stale" {
				stored["review_after"] = "2000-01-01"
			}
			if mode == "bad-date" {
				stored["review_after"] = "not a date"
			}
			if mode == "superseded" {
				stored["superseded_by_sync_id"] = "replacement"
			}
			write(stored)
		default:
			if r.URL.Path == "/sessions/"+session+"/end" {
				write(map[string]any{"id": session, "status": "completed"})
				return
			}
			w.WriteHeader(404)
		}
	})}
	go server.Serve(listener)
	ready := map[string]any{"protocol": "engram-session-owned-v1", "endpoint": endpoint, "pid": os.Getpid(), "instance_id": helperInstance}
	if mode == "pid" {
		ready["pid"] = os.Getpid() + 1
	}
	if mode == "endpoint" {
		ready["endpoint"] = "http://localhost:1234"
	}
	if mode == "extra" {
		ready["extra"] = true
	}
	if mode == "no-readiness" {
		// Deliberately wait for stdin closure without publishing readiness.
	} else if mode == "readiness-size" {
		fmt.Println(strings.Repeat("x", 1025))
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(ready)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	log("stdin-closed")
	_ = server.Close()
	os.Exit(0)
}

func fakeOptions(t *testing.T, mode string) (Options, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "engram")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec "+quote(binary)+" -test.run='^TestOwnedHelper$'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "trace")
	t.Setenv("KOARD_ENGRAM_HELPER", "1")
	t.Setenv("KOARD_ENGRAM_CASE", mode)
	t.Setenv("KOARD_ENGRAM_TRACE", trace)
	t.Setenv("ENGRAM_URL", "http://invalid")
	t.Setenv("ENGRAM_PROJECT", "foreign")
	t.Setenv("ENGRAM_CLOUD_TOKEN", "must-not-inherit")
	return Options{Executable: script, DataDir: filepath.Join(root, "data"), Workspace: root}, trace
}

func TestStartupRefusals(t *testing.T) {
	for _, mode := range []string{"pid", "endpoint", "extra", "readiness-size", "health", "version", "compatibility", "ambiguous", "cwd", "session"} {
		t.Run(mode, func(t *testing.T) {
			opts, trace := fakeOptions(t, mode)
			a, err := Start(context.Background(), opts)
			if err == nil {
				a.Close(context.Background())
				t.Fatal("unsafe startup accepted")
			}
			body, _ := os.ReadFile(trace)
			if strings.Contains(string(body), "POST /observations") {
				t.Fatal("startup saved memory")
			}
		})
	}
}

func TestStartupCancellationAndInvalidDirectories(t *testing.T) {
	opts, trace := fakeOptions(t, "no-readiness")
	for _, field := range []string{"workspace", "data"} {
		invalid := opts
		if field == "workspace" {
			invalid.Workspace = "relative"
		} else {
			invalid.DataDir = "relative"
		}
		if _, err := Start(context.Background(), invalid); err == nil {
			t.Fatalf("accepted relative %s", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, opts); err == nil {
		t.Fatal("accepted cancelled startup")
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatal("invalid startup invoked child")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Start(ctx, opts); err == nil {
		t.Fatal("accepted missing readiness")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled startup did not terminate child promptly")
	}
}

func TestOwnedStore(t *testing.T) {
	for _, mode := range []string{"ok", "stale", "superseded", "bad-date", "full-foreign", "full-id", "lost", "search-foreign", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			opts, trace := fakeOptions(t, mode)
			a, err := Start(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close(context.Background()) })
			project, err := a.Resolve(context.Background(), opts.Workspace)
			if err != nil || project.Name != "canonical-project" || project.Directory != opts.Workspace {
				t.Fatalf("resolve %+v %v", project, err)
			}
			if _, err := a.Search(context.Background(), "other", "title"); err == nil {
				t.Fatal("foreign query accepted")
			}
			items, searchErr := a.Search(context.Background(), project.Name, "title")
			if mode == "search-foreign" || mode == "redirect" || mode == "oversize" {
				if searchErr == nil {
					t.Fatal("unsafe search accepted")
				}
				return
			}
			if searchErr != nil || len(items) != 0 {
				t.Fatalf("empty search: %v %v", items, searchErr)
			}
			p := handoff.MemoryProposal{Type: "architecture", Title: "Boundary", Content: "Core is provider neutral."}
			item, err := a.Create(context.Background(), project.Name, "sha256:"+strings.Repeat("a", 64), p)
			wantError := mode == "lost" || mode == "bad-date" || mode == "full-foreign" || mode == "full-id"
			if (err != nil) != wantError {
				t.Fatalf("create %+v %v", item, err)
			}
			if !wantError {
				state := "active"
				if mode == "stale" || mode == "superseded" {
					state = "needs_review"
				}
				if item.ID != 1 || item.Content != p.Content || item.State != state {
					t.Fatalf("observation %+v", item)
				}
				items, err = a.Search(context.Background(), project.Name, "Boundary")
				if err != nil || len(items) != 1 || items[0].Content != p.Content || items[0].State != state {
					t.Fatalf("full search %+v %v", items, err)
				}
			}
			if err := a.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := a.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Search(context.Background(), project.Name, "Boundary"); err == nil {
				t.Fatal("closed store used")
			}
			body, _ := os.ReadFile(trace)
			lines := string(body)
			if strings.Count(lines, "POST /observations\n") != 1 || strings.Count(lines, "/end\n") != 1 || !strings.HasSuffix(lines, "stdin-closed\n") {
				t.Fatalf("side effects: %s", lines)
			}
		})
	}
}

func TestIsolatedEngram372(t *testing.T) {
	if testing.Short() {
		t.Skip("external Engram process")
	}
	binary, err := exec.LookPath("engram")
	if err != nil {
		t.Skip("Engram binary unavailable")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "engram 3.7.2" {
		t.Skip("integration requires Engram 3.7.2")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, err := Start(ctx, Options{Executable: binary, DataDir: filepath.Join(root, "data"), Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(context.Background())
	p, err := a.Resolve(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	proposal := handoff.MemoryProposal{Type: "discovery", Title: "Isolated adapter probe", Content: "This record exists only in temporary integration storage."}
	item, err := a.Create(ctx, p.Name, "koard-test-operation", proposal)
	if err != nil || item.Project != p.Name || item.Content != proposal.Content {
		t.Fatalf("create %+v %v", item, err)
	}
	items, err := a.Search(ctx, p.Name, "Isolated adapter probe")
	if err != nil || len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("search %+v %v", items, err)
	}
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
