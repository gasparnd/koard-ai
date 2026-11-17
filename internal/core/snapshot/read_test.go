package snapshot_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

func TestReadReferenceConfinementAndDigest(t *testing.T) {
	root := t.TempDir()
	data := []byte("inspected source")
	if err := os.WriteFile(filepath.Join(root, "source"), data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	for _, uri := range []string{"repo://source", "repo://../escape", "https://example.invalid/source", "repo://source/../source", "repo://outside"} {
		t.Run(uri, func(t *testing.T) {
			if uri == "repo://outside" {
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
					t.Fatal(err)
				}
			}
			got, err := snapshot.ReadReference(context.Background(), root, handoff.Reference{URI: uri, Digest: digest})
			if uri == "repo://source" {
				if err != nil || string(got) != string(data) {
					t.Fatalf("%s %v", got, err)
				}
			} else if err == nil {
				t.Fatal("unsafe reference accepted")
			}
		})
	}
	if _, err := snapshot.ReadReference(context.Background(), root, handoff.Reference{URI: "repo://source", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(nil))}); err == nil {
		t.Fatal("wrong digest accepted")
	}
}
