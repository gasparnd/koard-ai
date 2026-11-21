package cli

import (
	"os"
	"strings"
	"testing"
)

func TestSkillCreateIndependentNameBoundaries(t *testing.T) {
	for _, name := range []string{"a", "0", strings.Repeat("a", 64)} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			code, _, diagnostic := skillCall(t, root, "skill", "create", name)
			if code != 0 || diagnostic != "" {
				t.Fatalf("code=%d diagnostic=%q", code, diagnostic)
			}
		})
	}
}

func TestSkillCreateIndependentTrailingOption(t *testing.T) {
	root := t.TempDir()
	code, out, diagnostic := skillCall(t, root, "skill", "create", "valid", "--json")
	want := "koard skill create: expected one lowercase hyphenated name " +
		"(at most 64 characters), not a path or option\n"
	if code != 2 || out != "" || diagnostic != want {
		t.Fatalf("got (%d, %q, %q)", code, out, diagnostic)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected command changed workspace: %v, %v", entries, err)
	}
}
