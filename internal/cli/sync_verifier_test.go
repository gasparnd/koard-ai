package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/application/oddworkflow"
	"github.com/koard-ai/koard/internal/application/skillsync"
	"github.com/koard-ai/koard/internal/cli"
	"github.com/koard-ai/koard/internal/config"
)

func verifierWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0644); err != nil {
		t.Fatal(err)
	}
}

func verifierFixture(t *testing.T, sync string) (string, string) {
	t.Helper()
	p, canonicalErr := filepath.EvalSymlinks(t.TempDir())
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	var out, err bytes.Buffer
	if cli.Run([]string{"init"}, p, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	raw, e := os.ReadFile(filepath.Join(p, ".koard/koard.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"koard-planning", "koard-verification"} {
		data, e := os.ReadFile(filepath.Join("../../skills", name, "SKILL.md"))
		if e != nil {
			t.Fatal(e)
		}
		verifierWrite(t, filepath.Join(p, "skills", name, "SKILL.md"), string(data))
	}
	verifierWrite(t, filepath.Join(p, ".koard/koard.yaml"), string(raw)+sync)
	return p, string(raw)
}

func verifierState(t *testing.T, p string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p, path)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			result[rel] = "link:" + target
		case d.IsDir():
			result[rel] = "dir"
		default:
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			result[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Rejected declarations preserve every file, directory and link.
func TestVerifierSyncRejectedDeclarations(t *testing.T) {
	for _, tt := range []struct{ name, tail, want string }{
		{"missing-last", "- name: absent\n      runtimes: [codex]", "preflight skill \"absent\": project skill \"absent\" not found; drafts and global skills are not projected"},
		{"unknown-runtime", "- name: koard-verification\n      runtimes: [claude]", "unsupported runtime \"claude\" for skill \"koard-verification\"; use opencode or codex"},
		{"duplicate", "- name: koard-planning\n      runtimes: [codex]", "invalid configuration: sync.skills contains duplicate name \"koard-planning\""},
		{"empty-runtimes", "- name: koard-verification\n      runtimes: []", "invalid configuration: sync.skills[1].runtimes must not be empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := verifierFixture(t, "sync:\n  skills:\n    - name: koard-planning\n      runtimes: [opencode]\n    "+tt.tail+"\n")
			before := verifierState(t, p)
			for _, args := range [][]string{{"sync"}, {"sync", "--dry-run"}} {
				var out, err bytes.Buffer
				code := cli.Run(args, p, &out, &err)
				if code != 1 || out.String() != "" || err.String() != "koard sync: "+tt.want+"\n" {
					t.Fatalf("%v: %d %q %q", args, code, out.String(), err.String())
				}
				if !reflect.DeepEqual(before, verifierState(t, p)) {
					t.Fatal("rejected command mutated project")
				}
			}
		})
	}
}

type verifierLateTarget struct {
	root    string
	calls   int
	failure error
}

func (v *verifierLateTarget) SkillDirectory() string {
	v.calls++
	if v.calls == 2 {
		v.failure = os.MkdirAll(filepath.Join(v.root, ".agents/skills/koard-verification"), 0755)
	}
	return ".agents/skills"
}

type verifierFixedTarget string

func (v verifierFixedTarget) SkillDirectory() string { return string(v) }

// A deterministic post-preflight conflict preserves completed links; retry converges.
func TestVerifierSyncLateFailureRetry(t *testing.T) {
	p, _ := verifierFixture(t, "")
	late := &verifierLateTarget{root: p}
	req := []skillsync.Request{{Name: "koard-planning", Target: verifierFixedTarget(".opencode/skills")}, {Name: "koard-verification", Target: late}}
	got, err := skillsync.Run(p, req, false)
	if late.failure != nil {
		t.Fatal(late.failure)
	}
	want := fmt.Sprintf("stopped after 1 of 2 projections at skill %q: projection destination already exists: %s; previous links are kept; resolve the cause and rerun sync", "koard-verification", filepath.Join(p, ".agents/skills/koard-verification"))
	if err == nil || err.Error() != want || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	link := filepath.Join(p, ".opencode/skills/koard-planning")
	original, e := os.Readlink(link)
	if e != nil {
		t.Fatal(e)
	}
	if err := os.Remove(filepath.Join(p, ".agents/skills/koard-verification")); err != nil {
		t.Fatal(err)
	}
	got, err = skillsync.Run(p, req, false)
	if err != nil || len(got) != 2 || !got[0].Existing || got[1].Existing {
		t.Fatalf("%+v %v", got, err)
	}
	after, e := os.Readlink(link)
	if e != nil || after != original {
		t.Fatal("first link changed", e)
	}
	before := verifierState(t, p)
	got, err = skillsync.Run(p, req, false)
	if err != nil || !got[0].Existing || !got[1].Existing || !reflect.DeepEqual(before, verifierState(t, p)) {
		t.Fatalf("retry mutated state: %+v %v", got, err)
	}
}

// Valid sync is part of ODD's configuration identity, never an implicit action.
func TestVerifierODDSyncIdentity(t *testing.T) {
	declaration := "sync:\n  skills:\n    - name: koard-planning\n      runtimes: [codex]\n"
	p, base := verifierFixture(t, declaration)
	path := filepath.Join(p, ".koard/koard.yaml")
	raw := strings.ReplaceAll(base, "provider: unset", "provider: codex")
	raw = strings.ReplaceAll(raw, "model: unset", "model: verifier-model") + declaration
	verifierWrite(t, path, raw)
	cfg, err := config.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	before := verifierState(t, p)
	prepared, err := oddworkflow.Prepare(context.Background(), p, "probe", "task", "verify", "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Configuration.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(raw))) || len(prepared.Roles) != 2 || prepared.Roles[0].Role != "executor" || prepared.Roles[1].Role != "verifier" {
		t.Fatalf("bad preparation: %+v", prepared)
	}
	for _, role := range prepared.Roles {
		if role.Provider != "codex" || role.Model != "verifier-model" {
			t.Fatalf("changed role: %+v", role)
		}
	}
	if !reflect.DeepEqual(before, verifierState(t, p)) {
		t.Fatal("prepare wrote state")
	}
	cfg.Sync.Skills[0].Runtimes[0] = "opencode"
	_, err = oddworkflow.Prepare(context.Background(), p, "probe", "task", "verify", "", cfg)
	if err == nil || err.Error() != "configuration changed during ODD preflight" {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, verifierState(t, p)) {
		t.Fatal("drift refusal wrote state")
	}
}
