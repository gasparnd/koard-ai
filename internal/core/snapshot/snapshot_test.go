package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/core/handoff"
)

const candidateID = "sha256:candidate"

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func digest(content string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
}

func repo(path string) handoff.Reference {
	return handoff.Reference{Kind: "source", URI: "repo://" + path}
}

func TestCaptureStoresBytesAndManifest(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "internal/a.go", "package a\n")
	writeFile(t, workspace, "docs/b.md", "# B\n")
	dir := filepath.Join(t.TempDir(), "snapshot")

	snap, err := Capture(context.Background(), workspace, dir, candidateID, []handoff.Reference{repo("internal/a.go"), repo("docs/b.md")})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	if snap.ID != fmt.Sprintf("sha256:%x", sha256.Sum256(manifestBytes)) {
		t.Fatalf("snapshot ID %q is not the digest of the stored manifest", snap.ID)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != CurrentSchemaVersion || manifest.CandidateID != candidateID {
		t.Fatalf("unexpected manifest header: %#v", manifest)
	}
	expected := []Entry{
		{Kind: "source", URI: "repo://internal/a.go", Digest: digest("package a\n"), Size: int64(len("package a\n"))},
		{Kind: "source", URI: "repo://docs/b.md", Digest: digest("# B\n"), Size: int64(len("# B\n"))},
	}
	if len(manifest.Entries) != len(expected) {
		t.Fatalf("expected %d entries, got %#v", len(expected), manifest.Entries)
	}
	for index, entry := range expected {
		if manifest.Entries[index] != entry {
			t.Fatalf("entry %d is %#v, expected %#v", index, manifest.Entries[index], entry)
		}
		blob := filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(entry.Digest, "sha256:"))
		info, err := os.Stat(blob)
		if err != nil {
			t.Fatalf("blob for %s was not stored: %v", entry.URI, err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("blob for %s is writable: %v", entry.URI, info.Mode())
		}
	}
}

func TestCaptureIdentityFollowsBytes(t *testing.T) {
	t.Parallel()

	capture := func(content string) string {
		t.Helper()
		workspace := t.TempDir()
		writeFile(t, workspace, "a.go", content)
		snap, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "snapshot"), candidateID, []handoff.Reference{repo("a.go")})
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		return snap.ID
	}

	if capture("one") != capture("one") {
		t.Fatal("identical bytes produced different snapshot identities")
	}
	if capture("one") == capture("two") {
		t.Fatal("different bytes behind the same reference produced the same identity")
	}
}

func TestCaptureBindsTheCandidate(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "a.go", "same")
	first, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "s"), "sha256:one", []handoff.Reference{repo("a.go")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "s"), "sha256:two", []handoff.Reference{repo("a.go")})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("two candidates with the same bytes share a snapshot identity")
	}
}

func TestCaptureAcceptsMatchingDeclaredDigest(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "a.go", "content")
	reference := repo("a.go")
	reference.Digest = digest("content")

	if _, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "s"), candidateID, []handoff.Reference{reference}); err != nil {
		t.Fatalf("a matching declared digest was refused: %v", err)
	}
}

func TestCaptureWithoutArtifactsRecordsAnEmptyManifest(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "s")
	snap, err := Capture(context.Background(), t.TempDir(), dir, candidateID, nil)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	encoded, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"entries":[]`) {
		t.Fatalf("an empty snapshot must list no entries explicitly: %s", encoded)
	}
	if _, err := Load(dir, snap.ID); err != nil {
		t.Fatalf("empty snapshot does not load: %v", err)
	}
}

func TestCaptureRefusesUnsafeOrUnboundArtifacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()
	writeFile(t, workspace, "a.go", "content")
	writeFile(t, workspace, "dir/inside.go", "inside")
	writeFile(t, outside, "secret", "secret")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(workspace, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "linkdir")); err != nil {
		t.Fatal(err)
	}

	mismatch := repo("a.go")
	mismatch.Digest = digest("other")

	cases := map[string]handoff.Reference{
		"other scheme":       {Kind: "proposal", URI: "koard://proposals/task-1"},
		"empty path":         repo(""),
		"absolute path":      repo("/etc/hosts"),
		"parent traversal":   repo("../secret"),
		"inner traversal":    repo("dir/../../secret"),
		"backslash path":     repo(`dir\inside.go`),
		"symlink escape":     repo("escape"),
		"symlinked dir":      repo("linkdir/secret"),
		"directory":          repo("dir"),
		"missing file":       repo("missing.go"),
		"digest mismatch":    mismatch,
		"malformed digest":   {Kind: "source", URI: "repo://a.go", Digest: "sha256:proposal"},
		"uppercase scheme":   {Kind: "source", URI: "REPO://a.go"},
		"non-canonical path": repo("./a.go"),
	}
	for name, reference := range cases {
		dir := filepath.Join(t.TempDir(), "snapshot")
		_, err := Capture(context.Background(), workspace, dir, candidateID, []handoff.Reference{reference})
		if err == nil {
			t.Errorf("%s: %q was captured", name, reference.URI)
			continue
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%q", reference.URI)) {
			t.Errorf("%s: error does not name the artifact: %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "manifest.json")); !os.IsNotExist(statErr) {
			t.Errorf("%s: a refused snapshot left a manifest behind", name)
		}
	}
}

func TestLoadVerifiesStoredSnapshot(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (string, Snapshot) {
		t.Helper()
		workspace := t.TempDir()
		writeFile(t, workspace, "a.go", "content")
		dir := filepath.Join(t.TempDir(), "snapshot")
		snap, err := Capture(context.Background(), workspace, dir, candidateID, []handoff.Reference{repo("a.go")})
		if err != nil {
			t.Fatal(err)
		}
		return dir, snap
	}

	t.Run("intact", func(t *testing.T) {
		t.Parallel()
		dir, snap := setup(t)
		loaded, err := Load(dir, snap.ID)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if loaded.ID != snap.ID || loaded.Manifest.CandidateID != candidateID || len(loaded.Manifest.Entries) != 1 {
			t.Fatalf("loaded snapshot differs: %#v", loaded)
		}
	})

	t.Run("wrong identity", func(t *testing.T) {
		t.Parallel()
		dir, _ := setup(t)
		if _, err := Load(dir, "sha256:other"); err == nil {
			t.Fatal("a snapshot loaded under an identity it does not have")
		}
	})

	t.Run("tampered blob", func(t *testing.T) {
		t.Parallel()
		dir, snap := setup(t)
		blob := filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(snap.Manifest.Entries[0].Digest, "sha256:"))
		if err := os.Chmod(blob, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(blob, []byte("changed"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, snap.ID); err == nil {
			t.Fatal("a tampered blob was accepted")
		}
	})

	t.Run("missing blob", func(t *testing.T) {
		t.Parallel()
		dir, snap := setup(t)
		blob := filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(snap.Manifest.Entries[0].Digest, "sha256:"))
		if err := os.Remove(blob); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, snap.ID); err == nil {
			t.Fatal("a snapshot with a missing blob was accepted")
		}
	})

	t.Run("trailing data", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "snapshot")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		forged := `{"schema_version":1,"candidate_id":"c","entries":[]} garbage`
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(forged), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, digest(forged)); err == nil {
			t.Fatal("a manifest with trailing data was accepted")
		}
	})

	t.Run("tampered manifest", func(t *testing.T) {
		t.Parallel()
		dir, snap := setup(t)
		path := filepath.Join(dir, "manifest.json")
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(encoded), candidateID, "sha256:forged", 1)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir, snap.ID); err == nil {
			t.Fatal("a tampered manifest was accepted")
		}
	})
}

func writeSized(t *testing.T, root, name string, size int64) {
	t.Helper()
	file, err := os.Create(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureRefusesAnArtifactOverTheSizeLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeSized(t, workspace, "at-limit.bin", MaxArtifactBytes)
	writeSized(t, workspace, "over-limit.bin", MaxArtifactBytes+1)

	if _, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "s"), candidateID, []handoff.Reference{repo("at-limit.bin")}); err != nil {
		t.Fatalf("an artifact at the limit was refused: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "s")
	_, err := Capture(context.Background(), workspace, dir, candidateID, []handoff.Reference{repo("over-limit.bin")})
	if err == nil || !strings.Contains(err.Error(), `"repo://over-limit.bin"`) {
		t.Fatalf("an artifact over the limit was not refused by name: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused oversized artifact left a manifest")
	}
}

func TestCaptureRefusesASnapshotOverTheTotalLimit(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeSized(t, workspace, "part.bin", MaxArtifactBytes)
	count := int(MaxSnapshotBytes/MaxArtifactBytes) + 1
	artifacts := make([]handoff.Reference, count)
	for index := range artifacts {
		artifacts[index] = repo("part.bin")
	}

	dir := filepath.Join(t.TempDir(), "s")
	if _, err := Capture(context.Background(), workspace, dir, candidateID, artifacts); err == nil {
		t.Fatal("a snapshot over the total limit was captured")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused oversized snapshot left a manifest")
	}
	if _, err := Capture(context.Background(), workspace, filepath.Join(t.TempDir(), "s"), candidateID, artifacts[:count-1]); err != nil {
		t.Fatalf("a snapshot at the total limit was refused: %v", err)
	}
}

func TestCaptureStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeFile(t, workspace, "a.go", "content")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dir := filepath.Join(t.TempDir(), "s")
	_, err := Capture(ctx, workspace, dir, candidateID, []handoff.Reference{repo("a.go")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cancellation error, got %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatal("a cancelled capture wrote to the snapshot directory")
	}
}
