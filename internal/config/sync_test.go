package config

import (
	"strings"
	"testing"
)

func TestOptionalSyncConfiguration(t *testing.T) {
	for _, extra := range []string{"", "sync: {}\n", "sync:\n  skills: []\n"} {
		parsed, err := Parse(strings.NewReader(validConfiguration + extra))
		if err != nil || len(parsed.Sync.Skills) != 0 {
			t.Fatalf("empty sync: %+v %v", parsed.Sync, err)
		}
	}
	parsed, err := Parse(strings.NewReader(validConfiguration + "sync:\n  skills:\n    - name: test-skill\n      runtimes: [opencode, codex]\n"))
	if err != nil || len(parsed.Sync.Skills) != 1 || parsed.Sync.Skills[0].Name != "test-skill" || len(parsed.Sync.Skills[0].Runtimes) != 2 {
		t.Fatalf("explicit sync: %+v %v", parsed.Sync, err)
	}
	for _, entry := range []string{
		"    - name: ''\n      runtimes: [codex]\n",
		"    - name: test\n      runtimes: []\n",
		"    - name: test\n      runtimes: [codex, codex]\n",
		"    - name: test\n      runtimes: ['']\n",
		"    - name: test\n      runtimes: [codex]\n    - name: test\n      runtimes: [opencode]\n",
		"    - name: test\n      runtimes: [codex]\n      unknown: true\n",
	} {
		if _, err := Parse(strings.NewReader(validConfiguration + "sync:\n  skills:\n" + entry)); err == nil {
			t.Fatalf("invalid sync accepted: %s", entry)
		}
	}
}
