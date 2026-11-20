package opencode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
)

func init() {
	if os.Getenv("VERIFIER_PWD_CHILD") != "1" {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("2.2.6")
		os.Exit(0)
	}
	die := func(s string) { fmt.Fprint(os.Stderr, s); os.Exit(23) }
	cwd, _ := os.Getwd()
	a, e := os.Stat(cwd)
	if e != nil {
		die("cwd")
	}
	b, e := os.Stat(os.Getenv("PWD"))
	if e != nil || !os.SameFile(a, b) {
		die("wrong PWD")
	}
	if os.Getenv("OPENCODE_CONFIG_CONTENT") != proposalPolicy || os.Getenv("VERIFIER_SENTINEL") != "kept=value" {
		die("environment")
	}
	data, e := os.ReadFile(filepath.Join(os.Getenv("PWD"), "change.go"))
	if e != nil || string(data) != os.Getenv("VERIFIER_BYTES") {
		die("wrong bytes")
	}
	if e = os.WriteFile(os.Getenv("VERIFIER_SEEN"), []byte(cwd), 0600); e != nil {
		die("record")
	}
	switch os.Getenv("VERIFIER_MODE") {
	case "error":
		die("deliberate failure")
	case "cancel":
		time.Sleep(30 * time.Second)
	}
	fmt.Print(os.Getenv("VERIFIER_OUTPUT"))
	os.Exit(0)
}

func TestVerifierPWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PWD and signal assertions use Unix conventions")
	}
	for _, op := range []string{"execute", "review"} {
		for _, mode := range []string{"stale", "empty", "absent", "error", "cancel"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				live := t.TempDir()
				file := filepath.Join(live, "change.go")
				if err := os.WriteFile(file, []byte("live drift\n"), 0600); err != nil {
					t.Fatal(err)
				}
				req, store := validReviewRequest(t)
				before := map[string]string{}
				if err := filepath.Walk(store, func(p string, i os.FileInfo, e error) error {
					if e != nil {
						return e
					}
					if !i.IsDir() {
						b, err := os.ReadFile(p)
						if err != nil {
							return err
						}
						before[p] = string(b)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PWD", live)
				if mode == "stale" {
					t.Setenv("PWD", t.TempDir())
				}
				if mode == "empty" {
					t.Setenv("PWD", "")
				}
				if mode == "absent" {
					if err := os.Unsetenv("PWD"); err != nil {
						t.Fatal(err)
					}
				}
				pwd, has := os.LookupEnv("PWD")
				t.Setenv("OPENCODE_CONFIG_CONTENT", "hostile inherited policy")
				t.Setenv("VERIFIER_SENTINEL", "kept=value")
				t.Setenv("VERIFIER_PWD_CHILD", "1")
				t.Setenv("VERIFIER_MODE", mode)
				seen := filepath.Join(t.TempDir(), "seen")
				t.Setenv("VERIFIER_SEEN", seen)
				payload := validResultJSON("handoff-1", handoff.RoleExecutor)
				want := "live drift\n"
				if op == "review" {
					want = "package change\n"
					payload = mustMarshalReviewResult(t, review.Result{SchemaVersion: 1, RequestID: req.ID, Attempt: req.Attempt, Target: req.Target, Verdict: review.VerdictApproved})
				}
				t.Setenv("VERIFIER_BYTES", want)
				t.Setenv("VERIFIER_OUTPUT", string(transcript(payload)))
				exe, e := os.Executable()
				if e != nil {
					t.Fatal(e)
				}
				a, e := New(Config{Executable: exe, Workspace: live, Model: "test/model"})
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if mode == "cancel" {
					go func() {
						for {
							if _, e := os.Stat(seen); e == nil {
								cancel()
								return
							}
							select {
							case <-ctx.Done():
								return
							case <-time.After(time.Millisecond * 5):
							}
						}
					}()
				}
				if op == "execute" {
					_, e = a.Execute(ctx, validRequest())
				} else {
					_, e = a.Review(ctx, req, store)
				}
				if mode == "error" {
					action := "run OpenCode proposal"
					if op == "review" {
						action = "run OpenCode review"
					}
					if e == nil || e.Error() != action+": deliberate failure: exit status 23" {
						t.Fatalf("error=%v", e)
					}
				} else if mode == "cancel" {
					if e == nil || !strings.Contains(e.Error(), "signal: killed") {
						t.Fatalf("cancel=%v", e)
					}
				} else if e != nil {
					t.Fatal(e)
				}
				observed, e := os.ReadFile(seen)
				if e != nil {
					t.Fatal(e)
				}
				if op == "review" {
					if _, e := os.Stat(string(observed)); !os.IsNotExist(e) {
						t.Fatal("review not cleaned")
					}
				}
				b, e := os.ReadFile(file)
				if e != nil || string(b) != "live drift\n" {
					t.Fatal("live changed")
				}
				for p, want := range before {
					b, e := os.ReadFile(p)
					if e != nil || string(b) != want {
						t.Fatal("snapshot changed")
					}
				}
				if got, set := os.LookupEnv("PWD"); got != pwd || set != has {
					t.Fatal("parent PWD changed")
				}
				if os.Getenv("OPENCODE_CONFIG_CONTENT") != "hostile inherited policy" {
					t.Fatal("parent policy changed")
				}
			})
		}
	}
}
