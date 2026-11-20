package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// finalText decodes OpenCode's NDJSON transport, not the model's result schema.
// Text events contain suffixes (including reconciliation after step_finish).
// Step-start timestamps order messages: late reconciliation of an older step
// must not replace the final answer. Ambiguous ordering is refused, not guessed.
// Source: OpenCode v2.2.3/v2.2.6 packages/cli/src/run/noninteractive.ts.
func finalText(output []byte) ([]byte, error) {
	type step struct {
		started, finished bool
		start             int64
		reason            string
		text              bytes.Buffer
	}
	steps := map[string]*step{}
	session := ""
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 16*1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var event struct {
			Type      string `json:"type"`
			Session   string `json:"sessionID"`
			Timestamp int64  `json:"timestamp"`
			Part      struct {
				Type    string  `json:"type"`
				Session string  `json:"sessionID"`
				Message string  `json:"messageID"`
				Text    *string `json:"text"`
				Reason  string  `json:"reason"`
			} `json:"part"`
		}
		// Unknown envelope metadata is forward-compatible; the model payload
		// remains subject to the existing strict domain decoder below this layer.
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, errors.New("invalid OpenCode event JSON")
		}
		if event.Session == "" || (session != "" && session != event.Session) {
			return nil, errors.New("OpenCode event session mismatch")
		}
		session = event.Session
		switch event.Type {
		case "error":
			return nil, errors.New("OpenCode reported a run error")
		case "retry", "background_wait":
			continue
		case "text", "step_start", "step_finish", "tool_use", "reasoning":
		default:
			return nil, errors.New("unsupported OpenCode event type")
		}
		p := event.Part
		if p.Session != session || p.Message == "" {
			return nil, errors.New("OpenCode part binding mismatch")
		}
		if event.Type == "tool_use" || event.Type == "reasoning" {
			continue
		}
		s := steps[p.Message]
		if s == nil {
			s = &step{}
			steps[p.Message] = s
		}
		switch event.Type {
		case "step_start":
			if p.Type != "step-start" || s.started || event.Timestamp <= 0 {
				return nil, errors.New("invalid OpenCode step start")
			}
			s.started, s.start = true, event.Timestamp
		case "step_finish":
			if p.Type != "step-finish" || s.finished || p.Reason == "" {
				return nil, errors.New("invalid OpenCode step finish")
			}
			s.finished, s.reason = true, p.Reason
		case "text":
			if p.Type != "text" || p.Text == nil {
				return nil, errors.New("invalid OpenCode text part")
			}
			s.text.WriteString(*p.Text)
		}
	}
	if scanner.Err() != nil {
		return nil, errors.New("OpenCode event exceeds transport limit")
	}
	var final *step
	starts := map[int64]bool{}
	for _, s := range steps {
		if !s.started || !s.finished {
			return nil, errors.New("incomplete OpenCode step")
		}
		if starts[s.start] {
			return nil, errors.New("ambiguous OpenCode step order")
		}
		starts[s.start] = true
		if final == nil || s.start > final.start {
			final = s
		}
	}
	if final == nil || final.reason != "stop" || len(bytes.TrimSpace(final.text.Bytes())) == 0 {
		return nil, errors.New("OpenCode has no completed final text")
	}
	return final.text.Bytes(), nil
}

func (adapter *Adapter) run(ctx context.Context, dir, prompt, action string) ([]byte, error) {
	arguments := []string{"run", "--standalone", "--archived", "--format", "json", "--model", adapter.model}
	if adapter.agent != "" {
		arguments = append(arguments, "--agent", adapter.agent)
	}
	arguments = append(arguments, prompt)
	result := adapter.runner.Run(ctx, command{Name: adapter.executable, Args: arguments, Dir: dir, Env: []string{"OPENCODE_CONFIG_CONTENT=" + proposalPolicy}})
	if result.err != nil {
		return nil, commandError(action, result)
	}
	text, err := finalText(result.stdout)
	if err != nil {
		return nil, fmt.Errorf("%s transport: %w", action, err)
	}
	return text, nil
}
