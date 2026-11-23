package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeSetupWithEngram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable")
	}
	for _, scenario := range []string{"install", "preview", "missing", "unsupported", "override", "plugin conflict", "plugin symlink", "config conflict", "existing bundle"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			t.Setenv("PATH", bin)
			t.Setenv("ENGRAM_BIN", "")
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			version := "engram 3.7.2"
			if scenario == "unsupported" {
				version = "engram 3.6.0"
			}
			if scenario != "missing" {
				body := "#!/bin/sh\n[ \"$*\" = version ] || exit 97\nprintf '%s\\n' '" + version + "'\n"
				if err := os.WriteFile(filepath.Join(bin, "engram"), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "override" {
				t.Setenv("ENGRAM_BIN", "/other/engram")
			}
			plugin := filepath.Join(root, ".opencode/plugins/engram.ts")
			if scenario == "plugin conflict" || scenario == "plugin symlink" {
				if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
					t.Fatal(err)
				}
				if scenario == "plugin symlink" {
					if err := os.Symlink("absent.ts", plugin); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(plugin, []byte("human plugin"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "config conflict" {
				if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out, errOut bytes.Buffer
			if scenario == "existing bundle" {
				if code := Run([]string{"native", "setup", "--runtime", "opencode"}, root, &out, &errOut); code != 0 {
					t.Fatal(code, errOut.String())
				}
			}
			before := nativeFiles(t, root)
			out.Reset()
			errOut.Reset()
			args := []string{"native", "setup", "--runtime", "opencode", "--with-engram"}
			if scenario == "preview" {
				args = append(args, "--dry-run")
			}
			code := Run(args, root, &out, &errOut)
			if scenario != "install" && scenario != "preview" {
				if code != 1 || errOut.Len() == 0 {
					t.Fatalf("expected preflight refusal: %d %s %s", code, &out, &errOut)
				}
				if got := nativeFiles(t, root); got != before {
					t.Fatal("refusal changed project files")
				}
				return
			}
			if code != 0 || errOut.Len() != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errOut)
			}
			if scenario == "preview" {
				if nativeFiles(t, root) != before || !strings.Contains(out.String(), "Would create .opencode/plugins/engram.ts") || strings.Contains(out.String(), "Engram configured") {
					t.Fatal("invalid preview", out.String())
				}
				return
			}
			data, err := os.ReadFile(filepath.Join(root, ".opencode/opencode.jsonc"))
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				DefaultAgent string `json:"default_agent"`
				MCP          struct {
					Servers map[string]struct {
						Type    string   `json:"type"`
						Command []string `json:"command"`
					} `json:"servers"`
				} `json:"mcp"`
			}
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			server := config.MCP.Servers["engram"]
			if config.DefaultAgent != "koard" || server.Type != "local" || strings.Join(server.Command, " ") != "engram mcp --tools=agent" {
				t.Fatal("incorrect native configuration", string(data))
			}
			for _, path := range []string{plugin, filepath.Join(root, ".opencode/engram/LICENSE"), filepath.Join(root, ".opencode/engram/README.md")} {
				if data, err := os.ReadFile(path); err != nil || len(data) == 0 {
					t.Fatalf("missing asset %s: %v", path, err)
				}
			}
			if !strings.Contains(out.String(), "Engram configured") || !strings.Contains(out.String(), "not verified") || !strings.Contains(out.String(), "global") {
				t.Fatal("missing proof limits", out.String())
			}
			before = nativeFiles(t, root)
			out.Reset()
			if code := Run(args, root, &out, &errOut); code != 0 || strings.Contains(out.String(), "Created") || nativeFiles(t, root) != before {
				t.Fatal("non-idempotent setup", code, &out, &errOut)
			}
		})
	}
}

func TestNativeSetupWithoutEngramDependency(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ENGRAM_BIN", "/unavailable/engram")
	var out, errOut bytes.Buffer
	root := t.TempDir()
	if code := Run([]string{"native", "setup", "--runtime", "opencode"}, root, &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("assets-only setup gained an Engram dependency: %d %s", code, &errOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode/plugins")); !os.IsNotExist(err) {
		t.Fatal("default setup installed a plugin")
	}
	if !strings.Contains(out.String(), "Native assets only;") {
		t.Fatal("default success output changed")
	}
	out.Reset()
	if code := Run([]string{"native", "setup", "--help"}, root, &out, &errOut); code != 0 || !strings.Contains(out.String(), "--with-engram") {
		t.Fatal("missing opt-in help")
	}
}

func TestNativeEngramVersionBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable")
	}
	for _, tc := range []struct{ name, body, message string }{
		{"failure", "echo private-detail >&2; exit 7", "check native Engram version: exit status 7"},
		{"overflow", "printf 'engram 3.7.2" + strings.Repeat(" ", 256) + "'", "native Engram requires version 3.7.2"},
		{"timeout", "exec /bin/sleep 5", "check native Engram version:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, bin, home := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", bin)
			t.Setenv("ENGRAM_BIN", "")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOME/calls\"\n" + tc.body + "\n"
			if err := os.WriteFile(filepath.Join(bin, "engram"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			before := nativeFiles(t, root)
			var out, errOut bytes.Buffer
			code := Run([]string{"native", "setup", "--runtime", "opencode", "--with-engram"}, root, &out, &errOut)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), tc.message) || strings.Contains(errOut.String(), "private-detail") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &errOut)
			}
			if nativeFiles(t, root) != before {
				t.Fatal("rejected setup changed project files")
			}
			calls, err := os.ReadFile(filepath.Join(home, "calls"))
			if err != nil || string(calls) != "version\n" {
				t.Fatalf("unexpected executable calls: %q %v", calls, err)
			}
		})
	}
}

// Include link targets and relative paths so refusal proves more than file count.
func nativeFiles(t *testing.T, root string) string {
	t.Helper()
	var result strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		result.WriteString(rel + "\n")
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result.WriteString(target)
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.String()
}
