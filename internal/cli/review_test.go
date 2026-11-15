package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests set KOARD_CONFIG_DIR, so they cannot run in parallel.

type reviewFixture struct {
	project   string
	configDir string
}

func newReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	fixture := reviewFixture{project: t.TempDir(), configDir: t.TempDir()}
	t.Setenv("KOARD_CONFIG_DIR", fixture.configDir)
	if code := Run([]string{"init"}, fixture.project, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("init exited with %d", code)
	}
	return fixture
}

func (fixture reviewFixture) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"review", "mode"}, args...), fixture.project, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (fixture reviewFixture) clonePath() string {
	return filepath.Join(fixture.project, ".koard", "state", "review-mode.json")
}

func (fixture reviewFixture) globalPath() string {
	return filepath.Join(fixture.configDir, "review-mode.json")
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat error: %v", path, err)
	}
}

func TestReviewModeStatusDefaultsToOnWithoutWriting(t *testing.T) {
	fixture := newReviewFixture(t)

	code, stdout, stderr := fixture.run(t, "status")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr)
	}
	expected := "Review mode: on (decided by default)\n  global: unset\n  clone: unset\n"
	if stdout != expected {
		t.Fatalf("unexpected status output:\n%s\nexpected:\n%s", stdout, expected)
	}
	assertMissing(t, fixture.clonePath())
	assertMissing(t, fixture.globalPath())
}

func TestReviewModeDisableDefaultsToCloneScope(t *testing.T) {
	fixture := newReviewFixture(t)

	code, stdout, stderr := fixture.run(t, "disable")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "Review mode set to off at clone scope.\n") {
		t.Fatalf("unexpected confirmation: %q", stdout)
	}
	if !strings.Contains(stdout, "Review mode: off (decided by clone)\n  global: unset\n  clone: off\n") {
		t.Fatalf("disable did not print the resulting status: %q", stdout)
	}
	if _, err := os.Stat(fixture.clonePath()); err != nil {
		t.Fatalf("clone setting was not written: %v", err)
	}
	assertMissing(t, fixture.globalPath())

	_, status, _ := fixture.run(t, "status")
	if !strings.HasPrefix(status, "Review mode: off (decided by clone)\n") {
		t.Fatalf("status does not read the clone setting back: %q", status)
	}
}

func TestReviewModeGlobalOffWinsOverCloneOn(t *testing.T) {
	fixture := newReviewFixture(t)

	if code, _, stderr := fixture.run(t, "disable", "--scope", "global"); code != 0 {
		t.Fatalf("disable global exited with %d: %s", code, stderr)
	}
	assertMissing(t, fixture.clonePath())

	code, stdout, stderr := fixture.run(t, "enable", "--scope=clone")
	if code != 0 {
		t.Fatalf("enable clone exited with %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Review mode: off (decided by global)\n  global: off\n  clone: on\n") {
		t.Fatalf("a clone on must not override a global off: %q", stdout)
	}
	if !strings.Contains(stdout, "The global scope keeps review mode off.") {
		t.Fatalf("output does not explain why the request had no effect: %q", stdout)
	}
}

func TestReviewModeCloneOffWinsOverGlobalOn(t *testing.T) {
	fixture := newReviewFixture(t)

	fixture.run(t, "enable", "--scope", "global")
	fixture.run(t, "disable", "--scope", "clone")

	_, stdout, _ := fixture.run(t, "status")
	if stdout != "Review mode: off (decided by clone)\n  global: on\n  clone: off\n" {
		t.Fatalf("unexpected status: %q", stdout)
	}
}

func TestReviewModeEnableGlobalRestoresOn(t *testing.T) {
	fixture := newReviewFixture(t)

	fixture.run(t, "disable", "--scope", "global")
	code, stdout, _ := fixture.run(t, "enable", "--scope", "global")
	if code != 0 {
		t.Fatalf("enable global exited with %d", code)
	}
	if !strings.Contains(stdout, "Review mode: on (decided by global)\n") {
		t.Fatalf("unexpected status after enabling: %q", stdout)
	}
	if strings.Contains(stdout, "keeps review mode") {
		t.Fatalf("an effective request must not print a note: %q", stdout)
	}
}

func TestReviewModeRejectsCorruptSetting(t *testing.T) {
	fixture := newReviewFixture(t)

	for _, content := range []string{
		`{"schema_version":1,"mode":"maybe"}`,
		`{"schema_version":2,"mode":"off"}`,
		`{"schema_version":1,"mode":"off","extra":true}`,
		`{"schema_version":1,"mode":"off"} {}`,
		`not json`,
	} {
		if err := os.WriteFile(fixture.clonePath(), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := fixture.run(t, "status")
		if code != 1 {
			t.Fatalf("content %q: expected exit code 1, got %d; stdout: %s", content, code, stdout)
		}
		if !strings.Contains(stderr, fixture.clonePath()) {
			t.Fatalf("content %q: error does not name the file: %q", content, stderr)
		}
		if stdout != "" {
			t.Fatalf("content %q: a failed status must not report a mode: %q", content, stdout)
		}
	}
}

func TestReviewModeCloneScopeRequiresKoardProject(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("KOARD_CONFIG_DIR", configDir)
	directory := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"review", "mode", "disable"}, directory, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "run 'koard init'") {
		t.Fatalf("error does not explain how to fix it: %q", stderr.String())
	}
	assertMissing(t, filepath.Join(directory, ".koard"))

	// Global scope does not depend on the current project.
	code = Run([]string{"review", "mode", "disable", "--scope", "global"}, directory, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("global scope outside a project exited with %d", code)
	}
}

func TestReviewModeRejectsRelativeConfigDir(t *testing.T) {
	t.Setenv("KOARD_CONFIG_DIR", "relative/koard")
	var stderr bytes.Buffer

	code := Run([]string{"review", "mode", "status"}, t.TempDir(), &bytes.Buffer{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "KOARD_CONFIG_DIR") {
		t.Fatalf("error does not name the variable: %q", stderr.String())
	}
}

func TestReviewModeUsageErrors(t *testing.T) {
	fixture := newReviewFixture(t)

	for _, args := range [][]string{
		{"review"},
		{"review", "policy"},
		{"review", "mode"},
		{"review", "mode", "toggle"},
		{"review", "mode", "enable", "--scope", "team"},
		{"review", "mode", "enable", "--scope"},
		{"review", "mode", "disable", "extra"},
		{"review", "mode", "status", "--scope", "clone"},
	} {
		var stderr bytes.Buffer
		code := Run(args, fixture.project, &bytes.Buffer{}, &stderr)
		if code != 2 {
			t.Errorf("%v: expected exit code 2, got %d", args, code)
		}
		if stderr.Len() == 0 {
			t.Errorf("%v: usage error printed nothing", args)
		}
	}
	assertMissing(t, fixture.clonePath())
	assertMissing(t, fixture.globalPath())
}

func TestReviewModeUsagePreservesModeSpecificHelp(t *testing.T) {
	fixture := newReviewFixture(t)
	var stderr bytes.Buffer
	code := Run([]string{"review", "mode", "toggle"}, fixture.project, &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	want := "koard review: unknown review mode action \"toggle\"\n" +
		"usage: koard review mode <status|enable|disable> [--scope clone|global]\n"
	if stderr.String() != want {
		t.Fatalf("unexpected mode usage:\nwant: %q\n got: %q", want, stderr.String())
	}
}

func TestHelpListsReviewMode(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if code := Run([]string{"help"}, t.TempDir(), &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("help exited with %d", code)
	}
	if !strings.Contains(stdout.String(), "review mode") {
		t.Fatalf("help does not list review mode: %q", stdout.String())
	}
}
