package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/koard-ai/koard/internal/skillregistry"
)

// Category winners retain their captured identity through both projections.
func TestIndependentProjectionCapture(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "skills", "category", "topic")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: topic\ndescription: Useful topic\n---\nKeep this instruction.\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, before, err := skillregistry.Capture(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{"opencode", "codex"} {
		var out, stderr bytes.Buffer
		code := Run([]string{"skill", "project", "--runtime", runtime, "topic"}, root, &out, &stderr)
		directory := ".opencode"
		if runtime == "codex" {
			directory = ".agents"
		}
		want := fmt.Sprintf("Projected %q -> %q\n", filepath.Join(root, directory, "skills", "topic"), source)
		if code != 0 || out.String() != want || stderr.Len() != 0 {
			t.Fatalf("%d %q %q", code, out.String(), stderr.String())
		}
		_, after, err := skillregistry.Capture(root, "")
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("capture changed: %v %#v", err, after)
		}
	}
}
