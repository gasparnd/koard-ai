package opencode

import (
	"context"
	"encoding/json"
	"github.com/koard-ai/koard/internal/core/handoff"
	"reflect"
	"strings"
	"testing"
)

func TestSkillInstructionsReachPromptWithoutPolicyChanges(t *testing.T) {
	request := validRequest()
	request.Objective += "\n\n---\r\nname: test\r\n---\r\nExact skill instructions."
	runner := &fakeRunner{responses: []commandResult{{stdout: []byte("opencode v2.2.6\n")}, {stdout: transcript(validResultJSON(request.ID, request.To))}}}
	if _, err := testAdapter(t, runner).Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	cmd := runner.commands[1]
	prompt := cmd.Args[len(cmd.Args)-1]
	_, body, ok := strings.Cut(prompt, "Handoff request:\n")
	var got handoff.Request
	if !ok || json.Unmarshal([]byte(body), &got) != nil || !reflect.DeepEqual(got, request) {
		t.Fatal("prompt changed request")
	}
	if !strings.Contains(prompt, "koard-skill:") {
		t.Fatal("selection reference contract missing")
	}
	if envValue(cmd.Env, "OPENCODE_CONFIG_CONTENT") != proposalPolicy {
		t.Fatal("policy changed")
	}
}
