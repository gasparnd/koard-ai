package skillregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCapturePreservesWinningBytesAndReport(t *testing.T) {
	root := t.TempDir()
	body := "---\r\nname: example\r\ndescription: Test skills\r\n---\r\nOriginal instructions.\r\n"
	for _, p := range []string{"skills/example/SKILL.md", ".claude/skills/example/SKILL.md"} {
		path := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	report, sources, err := Capture(root, "")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Scan(root, "", false)
	if err != nil || !reflect.DeepEqual(report, ordinary) {
		t.Fatalf("report drift: %v", err)
	}
	if len(sources) != 1 || sources[0].Content != body || sources[0].Skill != report.Skills[0] {
		t.Fatalf("sources: %+v", sources)
	}
	if err := os.WriteFile(sources[0].Skill.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if sources[0].Content != body {
		t.Fatal("capture changed")
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "Original instructions") {
		t.Fatal("list report leaked instruction body")
	}
}

func TestCaptureRejectsInvalidCatalog(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills/bad"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills/bad/SKILL.md"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	report, sources, err := Capture(root, "")
	if err == nil || !report.HasErrors() || len(sources) != 0 {
		t.Fatalf("partial catalog accepted: %v %+v", err, sources)
	}
}

func TestCaptureBoundsAggregateWithoutChangingScan(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			root := t.TempDir()
			count := MaxCatalogSkills + 1
			body := "Instructions"
			if large {
				count = 17
				body = strings.Repeat("x", MaxSkillBytes-128)
			}
			for i := 0; i < count; i++ {
				path := filepath.Join(root, "skills", fmt.Sprintf("s%03d", i), "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("---\ndescription: Test\n---\n"+body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			report, sources, err := Capture(root, "")
			if err == nil || len(sources) != 0 || !report.HasErrors() {
				t.Fatal("oversized capture accepted")
			}
			report, err = Scan(root, "", false)
			if err != nil || report.HasErrors() || len(report.Skills) != count {
				t.Fatal("metadata scan behavior changed")
			}
		})
	}
}
