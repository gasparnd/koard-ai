package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

// The test executable acts as both provider CLIs. It never starts a real model.
func init() {
	if os.Getenv("KOARD_TEST_SKILL_TRANSPORT") != "1" {
		return
	}
	if err := skillTransportProcess(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func skillTransportProcess() error {
	provider := filepath.Base(os.Args[0])
	args := os.Args[1:]
	if reflect.DeepEqual(args, []string{"--version"}) {
		if provider == "opencode" {
			fmt.Println("2.2.6")
		} else {
			fmt.Println("codex-cli 0.149.0")
		}
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("missing provider arguments")
	}
	prompt := args[len(args)-1]
	_, body, ok := strings.Cut(prompt, "Handoff request:\n")
	var req handoff.Request
	if !ok || json.Unmarshal([]byte(body), &req) != nil {
		return fmt.Errorf("invalid request prompt")
	}
	if !reflect.DeepEqual(req.Capabilities, []string{"repository:read"}) {
		return fmt.Errorf("capabilities changed")
	}
	if req.ExpectedOutput != (handoff.OutputContract{Format: "application/json", Schema: "koard://schemas/handoff-result/v1"}) {
		return fmt.Errorf("output contract changed")
	}
	var output string
	if provider == "opencode" {
		expected := []string{"run", "--standalone", "--archived", "--format", "json", "--model", "offline/" + string(req.To)}
		if !reflect.DeepEqual(args[:len(args)-1], expected) {
			return fmt.Errorf("OpenCode flags changed")
		}
		const policy = `{"snapshots":false,"policies":[{"action":"tool.use","resource":"*","effect":"deny"},{"action":"tool.use","resource":"read:*","effect":"allow"},{"action":"tool.use","resource":"glob:*","effect":"allow"},{"action":"tool.use","resource":"grep:*","effect":"allow"},{"action":"mcp.use","resource":"*","effect":"deny"},{"action":"skill.use","resource":"*","effect":"deny"},{"action":"plugin.use","resource":"*","effect":"deny"},{"action":"websearch.use","resource":"*","effect":"deny"}]}`
		if os.Getenv("OPENCODE_CONFIG_CONTENT") != policy {
			return fmt.Errorf("OpenCode policy changed")
		}
	} else if provider == "codex" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if len(args) < 2 {
			return fmt.Errorf("missing Codex output path")
		}
		output = args[len(args)-2]
		expected := []string{"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "-s", "read-only", "--color", "never", "-C", cwd, "-m", "offline/" + string(req.To), "--output-last-message", output}
		if !reflect.DeepEqual(args[:len(args)-1], expected) {
			return fmt.Errorf("Codex flags changed")
		}
	} else {
		return fmt.Errorf("unexpected provider %q", provider)
	}
	result := handoff.Result{SchemaVersion: 1, HandoffID: req.ID, Role: req.To, Status: handoff.StatusCompleted, Summary: "Offline fixture completed."}
	if strings.HasSuffix(req.ID, "-skills") {
		if !strings.Contains(prompt, "For an explicit skill-selection assignment only") || strings.Contains(req.Objective, "BODY-SENTINEL") {
			return fmt.Errorf("selection contract changed")
		}
		_, input, ok := strings.Cut(req.Objective, "Selection input:\n")
		var selection struct{ Catalog []struct{ ID, Digest string } }
		if !ok || json.Unmarshal([]byte(input), &selection) != nil {
			return fmt.Errorf("invalid catalog")
		}
		for _, e := range selection.Catalog {
			result.Artifacts = append(result.Artifacts, handoff.Reference{Kind: "skill", URI: "koard-skill:" + e.ID, Digest: e.Digest})
		}
	}
	log, err := os.OpenFile(os.Getenv("KOARD_TEST_SKILL_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(log).Encode(req)
	closeErr := log.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if provider == "codex" {
		return os.WriteFile(output, encoded, 0600)
	}
	for i, kind := range []string{"step_start", "text", "step_finish"} {
		part := map[string]any{"sessionID": "fixture", "messageID": "answer", "type": []string{"step-start", "text", "step-finish"}[i]}
		if kind == "text" {
			part["text"] = string(encoded)
		}
		if kind == "step_finish" {
			part["reason"] = "stop"
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"type": kind, "sessionID": "fixture", "timestamp": 1, "part": part}); err != nil {
			return err
		}
	}
	return nil
}

func TestSkillTransportMatrix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("provider helper executable names are Unix-specific")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"opencode", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, provider := range []string{"opencode", "codex"} {
		for _, command := range []string{"run", "odd"} {
			for _, policy := range []string{"fixed", "planned"} {
				for _, mode := range []string{"auto", "off", "empty"} {
					t.Run(strings.Join([]string{provider, command, policy, mode}, "/"), func(t *testing.T) {
						cwd := workflowProject(t)
						cwd, err = filepath.EvalSymlinks(cwd)
						if err != nil {
							t.Fatal(err)
						}
						home, tmp := t.TempDir(), t.TempDir()
						t.Setenv("HOME", home)
						t.Setenv("XDG_CONFIG_HOME", home)
						t.Setenv("TMPDIR", tmp)
						t.Setenv("PATH", bin)
						t.Setenv("KOARD_TEST_SKILL_TRANSPORT", "1")
						t.Setenv("GORACE", "atexit_sleep_ms=0")
						log := filepath.Join(tmp, "calls.jsonl")
						t.Setenv("KOARD_TEST_SKILL_LOG", log)
						text := strings.ReplaceAll(defaultConfig, "provider: engram", "provider: none")
						text = strings.ReplaceAll(text, "provider: unset", "provider: "+provider)
						for _, role := range []string{"planner", "executor", "verifier"} {
							text = strings.Replace(text, "model: unset", "model: offline/"+role, 1)
						}
						config := filepath.Join(cwd, ".koard/koard.yaml")
						if err := os.WriteFile(config, []byte(text), 0600); err != nil {
							t.Fatal(err)
						}
						content := "\ufeff---\r\nname: local\r\ndescription: inspect task\r\n---\r\nBODY-SENTINEL café\r\n"
						source := ""
						if mode != "empty" {
							if mode == "off" {
								content = "invalid skill"
							}
							source = skillFile(t, cwd, "skills/local/SKILL.md", content)
						}
						args := []string{"run", "--workflow", "transport", "--task", "task", "--objective", "Inspect tests", "--policy", policy}
						if command == "odd" {
							args = append([]string{"odd"}, args...)
						}
						if mode == "off" {
							args = append(args, "--skills=off")
						}
						var out, errOut bytes.Buffer
						if code := Run(args, cwd, &out, &errOut); code != 0 || errOut.Len() != 0 {
							t.Fatalf("code=%d out=%s err=%s", code, &out, &errOut)
						}
						if !strings.Contains(out.String(), "status: completed") {
							t.Fatal(out.String())
						}
						records, err := os.ReadFile(log)
						if err != nil {
							t.Fatal(err)
						}
						lines := bytes.Split(bytes.TrimSpace(records), []byte("\n"))
						roles := 2
						if policy == "planned" {
							roles = 3
						}
						want := roles
						if mode == "auto" {
							want *= 2
						}
						if len(lines) != want {
							t.Fatalf("calls=%d want=%d", len(lines), want)
						}
						dir := filepath.Join(cwd, ".koard/state/runs/transport")
						step := 0
						for _, line := range lines {
							var req handoff.Request
							if err := json.Unmarshal(line, &req); err != nil {
								t.Fatal(err)
							}
							if strings.HasSuffix(req.ID, "-skills") {
								if mode != "auto" {
									t.Fatal("unexpected selection")
								}
								continue
							}
							step++
							if mode == "auto" && !strings.Contains(req.Objective, content) {
								t.Fatal("instruction bytes changed")
							}
							persisted, err := os.ReadFile(filepath.Join(dir, "handoffs", fmt.Sprintf("%d-request.json", step)))
							if err != nil {
								t.Fatal(err)
							}
							var saved handoff.Request
							if err := json.Unmarshal(persisted, &saved); err != nil {
								t.Fatal(err)
							}
							if saved.Objective != req.Objective {
								t.Fatal("persisted Objective changed")
							}
							if mode == "auto" {
								delivery, err := os.ReadFile(filepath.Join(dir, "skills", string(req.To), "delivery.json"))
								if err != nil {
									t.Fatal(err)
								}
								if !bytes.Contains(delivery, []byte(fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(req.Objective))))) {
									t.Fatal("delivery digest mismatch")
								}
							}
						}
						if mode != "auto" {
							if _, err := os.Stat(filepath.Join(dir, "skills")); !os.IsNotExist(err) {
								t.Fatal("unexpected skill audit")
							}
						}
						if source != "" {
							b, err := os.ReadFile(source)
							if err != nil || string(b) != content {
								t.Fatal("source changed")
							}
						}
						b, err := os.ReadFile(config)
						if err != nil || string(b) != text {
							t.Fatal("configuration changed")
						}
						if command == "odd" {
							out.Reset()
							errOut.Reset()
							if code := Run([]string{"odd", "status", "--workflow", "transport"}, cwd, &out, &errOut); code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "consent_required") {
								t.Fatalf("status=%d %s %s", code, &out, &errOut)
							}
							after, err := os.ReadFile(log)
							if err != nil || !bytes.Equal(after, records) {
								t.Fatal("status invoked provider")
							}
						}
					})
				}
			}
		}
	}
}
