package engram

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
)

// This is direct real-provider MCP evidence, not OpenCode host-binding evidence.
func TestNativeProofEngramMCP(t *testing.T) {
	if testing.Short() || os.Getenv("KOARD_NATIVE_PROOF") != "1" {
		t.Skip("opt in with KOARD_NATIVE_PROOF=1; starts isolated Engram processes")
	}
	binary, err := exec.LookPath("engram")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	workspace := filepath.Join(root, "fixture")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dataDir := filepath.Join(root, "data")
	a, err := Start(ctx, Options{Executable: binary, DataDir: dataDir, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := a.Close(context.Background()); err != nil {
			t.Errorf("close owned Engram process: %v", err)
		}
	}()
	project, err := a.Resolve(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("Synthetic native proof preserves fixture ordering.\n", 200) + "END_OF_FULL_NATIVE_PROOF"
	item, err := a.Create(ctx, project.Name, "native-mcp-proof", handoff.MemoryProposal{
		Type: "discovery", Title: "Native deterministic retrieval fixture", Content: content,
	})
	if err != nil {
		t.Fatal(err)
	}

	mcpCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(mcpCtx, binary, "mcp", "--tools=agent")
	cmd.Dir = workspace
	cmd.Env = childEnvironment(dataDir, "")
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close(); stop(); _ = cmd.Wait() }()
	encoder := json.NewEncoder(in)
	decoder := json.NewDecoder(io.LimitReader(out, 1<<20))
	rpc := func(id int, method string, params any) json.RawMessage {
		t.Helper()
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for {
			var response struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := decoder.Decode(&response); err != nil {
				t.Fatalf("MCP %s: %v", method, err)
			}
			if response.ID == nil {
				continue
			}
			if *response.ID != id || (len(response.Error) != 0 && string(response.Error) != "null") {
				t.Fatalf("MCP %s rejected or mismatched: %s", method, response.Error)
			}
			return response.Result
		}
	}
	rpc(1, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "koard-native-proof", "version": "1"}})
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc(2, "tools/list", map[string]any{}), &catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range catalog.Tools {
		if tool.Name == "mem_get_observation" {
			found = true
		}
	}
	if !found {
		t.Fatal("installed MCP does not expose mem_get_observation")
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(rpc(3, "tools/call", map[string]any{"name": "mem_get_observation", "arguments": map[string]any{"id": item.ID, "project": project.Name}}), &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("full retrieval returned an MCP tool error")
	}
	var text strings.Builder
	for _, part := range result.Content {
		if part.Type != "text" {
			continue
		}
		var envelope struct {
			Result string `json:"result"`
		}
		if json.Unmarshal([]byte(part.Text), &envelope) == nil && envelope.Result != "" {
			text.WriteString(envelope.Result)
		} else {
			text.WriteString(part.Text)
		}
	}
	if !strings.Contains(text.String(), content) {
		t.Fatal("MCP did not return the exact full content and tail marker")
	}
	t.Log("Real isolated MCP returned all", len(content), "content bytes; no host binding or model invocation claimed")
}
