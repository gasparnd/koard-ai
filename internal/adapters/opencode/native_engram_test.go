package opencode

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
)

func TestNativeEngramBundleIntegrity(t *testing.T) {
	bundle, err := NativeEngramBundle()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		".opencode/plugins/engram.ts": "af31a431096dd53e5860c947c9460e4e84edd24cf04021e35fb4f198f8473632",
		".opencode/engram/LICENSE":    "09608597ddda4e5f9033ac407a0d401986d96376c47f6d46789ca38db672dc15",
	} {
		if got := fmt.Sprintf("%x", sha256.Sum256(bundle.Files[filepath.FromSlash(path)])); got != want {
			t.Fatalf("upstream asset %s drifted: %s", path, got)
		}
	}
	base, err := NativeBundle()
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Files) != 6 || len(bundle.Files) != 9 {
		t.Fatal("unexpected bundle inventory")
	}
	for path, data := range base.Files {
		if path != filepath.FromSlash(".opencode/opencode.jsonc") && string(bundle.Files[path]) != string(data) {
			t.Fatalf("optional integration changed %s", path)
		}
	}
}
