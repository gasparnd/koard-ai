package opencode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/application/nativeintegration"
)

// Evaluate real rules without a prompt, model, or reconstructed permission matcher.
// This does not claim that an attempted write tool reached the evaluator.
func TestNativeProofOpenCodePermissions(t *testing.T) {
	if testing.Short() || os.Getenv("KOARD_NATIVE_PROOF") != "1" {
		t.Skip("opt in with KOARD_NATIVE_PROOF=1; starts an isolated OpenCode server")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "fixture")
	for _, directory := range []string{workspace, filepath.Join(root, "home"), filepath.Join(root, "tmp"), filepath.Join(root, "config", "opencode")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := NativeBundle()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativeintegration.Setup(workspace, bundle, false); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(workspace, "canary.txt")
	if err := os.WriteFile(canary, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// These policies disable integrations, not the tool actions being evaluated.
	config := `{"update":"disable","policies":[
		{"action":"provider.use","resource":"*","effect":"deny"},
		{"action":"integration.connect","resource":"*","effect":"deny"},
		{"action":"integration.use","resource":"*","effect":"deny"},
		{"action":"mcp.use","resource":"*","effect":"deny"},
		{"action":"plugin.use","resource":"*","effect":"deny"},
		{"action":"websearch.use","resource":"*","effect":"deny"}]}`
	if err := os.WriteFile(filepath.Join(root, "config", "opencode", "opencode.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(passwordBytes)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Dir = workspace
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"),
		"TMPDIR=" + filepath.Join(root, "tmp"), "XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_DATA_HOME=" + filepath.Join(root, "data"), "XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "OPENCODE_DB=" + filepath.Join(root, "opencode.db"),
		"OPENCODE_PASSWORD=" + password,
	}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(out)
		endpoint := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)
		for scanner.Scan() {
			if value := endpoint.FindString(scanner.Text()); value != "" {
				ready <- value
				_, _ = io.Copy(io.Discard, out)
				return
			}
		}
		ready <- ""
	}()
	var base string
	select {
	case base = <-ready:
		if base == "" {
			t.Fatal("owned OpenCode server exited before readiness")
		}
	case <-ctx.Done():
		t.Fatal("owned OpenCode server readiness deadline")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
	defer client.CloseIdleConnections()
	request := func(method, path string, input any, output any) {
		t.Helper()
		var body io.Reader
		if input != nil {
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("opencode", password)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("owned API %s: %v", path, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("owned API %s returned HTTP %d; proof unavailable", path, response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
	var schema struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	request("GET", "/openapi.json", nil, &schema)
	if _, exists := schema.Paths["/api/session/{sessionID}/permission"]; !exists {
		t.Fatal("installed runtime does not advertise the permission evaluator")
	}
	var agents struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	request("GET", "/api/agent?"+url.Values{"location[directory]": {workspace}}.Encode(), nil, &agents)
	registered := map[string]bool{}
	for _, agent := range agents.Data {
		registered[agent.ID] = true
	}
	create := func(input any) string {
		t.Helper()
		var response struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		request("POST", "/api/session", input, &response)
		if response.Data.ID == "" {
			t.Fatal("runtime did not return a session identity")
		}
		return response.Data.ID
	}
	parent := create(map[string]any{"location": map[string]string{"directory": workspace}, "agent": "koard"})
	for _, agent := range []string{"koard-explorer", "koard-verifier"} {
		if !registered[agent] {
			t.Fatalf("shipped agent %s was not registered", agent)
		}
		child := create(map[string]string{"parentID": parent, "agent": agent})
		for _, probe := range []struct{ action, resource, want string }{{"edit", canary, "deny"}, {"shell", "printf changed", "deny"}, {"read", canary, "allow"}} {
			var result struct {
				Data struct {
					ID     string `json:"id"`
					Effect string `json:"effect"`
				} `json:"data"`
			}
			request("POST", "/api/session/"+child+"/permission", map[string]any{"agent": agent, "action": probe.action, "resources": []string{probe.resource}}, &result)
			if result.Data.ID == "" || result.Data.Effect != probe.want {
				t.Fatalf("%s %s: got %q, want %q", agent, probe.action, result.Data.Effect, probe.want)
			}
		}
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "unchanged\n" {
		t.Fatal("permission probe changed the canary")
	}
	t.Log("Real evaluator denied edit/shell and allowed read for both shipped inspection agents; no model or tool execution")
}
