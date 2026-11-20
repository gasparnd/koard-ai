package codex

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
	runner := promptRunner(t, "codex-cli 0.149.0", validResultJSON(request.ID, request.To))
	if _, err := testAdapter(t, runner).Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	args := runner.commands[1].Args
	prompt := args[len(args)-1]
	_, body, ok := strings.Cut(prompt, "Handoff request:\n")
	var got handoff.Request
	if !ok || json.Unmarshal([]byte(body), &got) != nil || !reflect.DeepEqual(got, request) {
		t.Fatal("prompt changed request")
	}
	if !strings.Contains(prompt, "koard-skill:") {
		t.Fatal("selection reference contract missing")
	}
	if !reflect.DeepEqual(args[1:1+len(policyFlags())], policyFlags()) {
		t.Fatal("policy changed")
	}
}
