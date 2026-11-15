// Package snapshot captures the exact bytes a candidate proposes. A snapshot
// is a content-addressed set of read-only blobs plus a manifest; its identity
// is the SHA-256 digest of the manifest, which binds the candidate identity
// and the digest of every captured file. Later review, consent, and receipts
// bind this identity instead of the references a responder declared.
package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/koard-ai/koard/internal/core/handoff"
)

// CurrentSchemaVersion is the only manifest version this package writes and
// reads.
const CurrentSchemaVersion = 1

// RepoScheme marks an artifact that names a file inside the workspace.
const RepoScheme = "repo://"

// The limits bound the memory a capture holds, because every artifact is read
// and checked before anything is written.
const (
	// MaxArtifactBytes is the largest single artifact a snapshot captures.
	MaxArtifactBytes int64 = 8 << 20
	// MaxSnapshotBytes is the largest total a snapshot captures, counting an
	// artifact once per time it is declared.
	MaxSnapshotBytes int64 = 32 << 20
)

const (
	manifestName = "manifest.json"
	digestPrefix = "sha256:"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Entry is one captured artifact, in the order the responder declared it.
type Entry struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Manifest binds a candidate to the bytes of its artifacts.
type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	CandidateID   string  `json:"candidate_id"`
	Entries       []Entry `json:"entries"`
}

// Snapshot is a stored manifest and its identity.
type Snapshot struct {
	ID       string
	Manifest Manifest
}

// Capture reads every artifact from workspace and stores it under dir. Every
// artifact is read and checked before anything is written, so a refused
// snapshot leaves no manifest behind. An existing manifest is never replaced.
// Cancellation is observed before each artifact is read and before each blob
// is published.
func Capture(ctx context.Context, workspace, dir, candidateID string, artifacts []handoff.Reference) (Snapshot, error) {
	if strings.TrimSpace(candidateID) == "" {
		return Snapshot{}, errors.New("snapshot requires a candidate identity")
	}
	root, err := realWorkspace(workspace)
	if err != nil {
		return Snapshot{}, err
	}

	entries := make([]Entry, 0, len(artifacts))
	contents := make([][]byte, 0, len(artifacts))
	var total int64
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, fmt.Errorf("capture stopped before artifact %q: %w", artifact.URI, err)
		}
		content, err := readArtifact(root, artifact)
		if err != nil {
			return Snapshot{}, fmt.Errorf("artifact %q: %w", artifact.URI, err)
		}
		total += int64(len(content))
		if total > MaxSnapshotBytes {
			return Snapshot{}, fmt.Errorf("artifact %q: snapshot exceeds %d bytes", artifact.URI, MaxSnapshotBytes)
		}
		entry := Entry{
			Kind:   artifact.Kind,
			URI:    artifact.URI,
			Digest: digestOf(content),
			Size:   int64(len(content)),
		}
		if artifact.Digest != "" && artifact.Digest != entry.Digest {
			return Snapshot{}, fmt.Errorf("artifact %q: declared digest %s does not match the captured bytes %s", artifact.URI, artifact.Digest, entry.Digest)
		}
		entries = append(entries, entry)
		contents = append(contents, content)
	}

	manifest := Manifest{SchemaVersion: CurrentSchemaVersion, CandidateID: candidateID, Entries: entries}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode snapshot manifest: %w", err)
	}

	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, fmt.Errorf("capture stopped before storing %q: %w", entry.URI, err)
		}
		if err := storeBlob(dir, entry.Digest, contents[index]); err != nil {
			return Snapshot{}, err
		}
	}
	if err := writeOnce(filepath.Join(dir, manifestName), encoded); err != nil {
		return Snapshot{}, fmt.Errorf("write snapshot manifest: %w", err)
	}
	return Snapshot{ID: digestOf(encoded), Manifest: manifest}, nil
}

// Load reads the snapshot stored under dir and verifies that the manifest has
// identity id and that every blob still matches its digest.
func Load(dir, id string) (Snapshot, error) {
	encoded, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	if actual := digestOf(encoded); actual != id {
		return Snapshot{}, fmt.Errorf("snapshot manifest has identity %s, expected %s", actual, id)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot manifest: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Snapshot{}, errors.New("decode snapshot manifest: trailing data after the manifest")
	}
	if manifest.SchemaVersion != CurrentSchemaVersion {
		return Snapshot{}, fmt.Errorf("snapshot manifest schema_version %d is not supported", manifest.SchemaVersion)
	}
	for _, entry := range manifest.Entries {
		if _, err := ReadEntry(dir, entry); err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{ID: id, Manifest: manifest}, nil
}

// ReadEntry returns the stored bytes of one entry after checking that they
// still match its digest and size.
func ReadEntry(dir string, entry Entry) ([]byte, error) {
	if !digestPattern.MatchString(entry.Digest) {
		return nil, fmt.Errorf("snapshot entry %q has malformed digest %q", entry.URI, entry.Digest)
	}
	content, err := os.ReadFile(blobPath(dir, entry.Digest))
	if err != nil {
		return nil, fmt.Errorf("snapshot entry %q: %w", entry.URI, err)
	}
	if digestOf(content) != entry.Digest || int64(len(content)) != entry.Size {
		return nil, fmt.Errorf("snapshot entry %q no longer matches its digest", entry.URI)
	}
	return content, nil
}

func realWorkspace(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory", root)
	}
	return root, nil
}

// readArtifact accepts only a canonical, relative repo:// path that resolves
// to a regular file inside root, and reads the file it validated.
func readArtifact(root string, artifact handoff.Reference) ([]byte, error) {
	if artifact.Digest != "" && !digestPattern.MatchString(artifact.Digest) {
		return nil, fmt.Errorf("declared digest %q is not a sha256 digest", artifact.Digest)
	}
	if !strings.HasPrefix(artifact.URI, RepoScheme) {
		return nil, fmt.Errorf("only %s artifacts can be captured", RepoScheme)
	}
	relative := strings.TrimPrefix(artifact.URI, RepoScheme)
	if relative == "" || strings.Contains(relative, `\`) || path.IsAbs(relative) ||
		path.Clean(relative) != relative || !filepath.IsLocal(filepath.FromSlash(relative)) {
		return nil, errors.New("path must be a canonical relative path inside the workspace")
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	inside, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(inside) {
		return nil, errors.New("path resolves outside the workspace")
	}
	expected, err := os.Lstat(resolved)
	if err != nil {
		return nil, fmt.Errorf("inspect path: %w", err)
	}
	if !expected.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if expected.Size() > MaxArtifactBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxArtifactBytes)
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open path: %w", err)
	}
	defer file.Close()
	// The file opened must be the file validated; a swap in between is refused.
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect path: %w", err)
	}
	if !os.SameFile(expected, opened) {
		return nil, errors.New("path changed while it was being captured")
	}
	// The file may grow after it was inspected, so the read is bounded too.
	content, err := io.ReadAll(io.LimitReader(file, MaxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read path: %w", err)
	}
	if int64(len(content)) > MaxArtifactBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxArtifactBytes)
	}
	return content, nil
}

func storeBlob(dir, digest string, content []byte) error {
	target := blobPath(dir, digest)
	if err := writeOnce(target, content); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("store snapshot blob %s: %w", digest, err)
		}
		// Content addressing deduplicates identical files, but only after the
		// stored bytes are proven to be the same.
		existing, readErr := os.ReadFile(target)
		if readErr != nil || digestOf(existing) != digest {
			return fmt.Errorf("snapshot blob %s already exists with different content", digest)
		}
	}
	return nil
}

// writeOnce publishes content at target read-only, atomically, and never over
// an existing file.
func writeOnce(target string, content []byte) error {
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary.Name(), 0o444); err != nil {
		return err
	}
	return os.Link(temporary.Name(), target)
}

func blobPath(dir, digest string) string {
	return filepath.Join(dir, "blobs", "sha256", strings.TrimPrefix(digest, digestPrefix))
}

func digestOf(content []byte) string {
	return fmt.Sprintf("%s%x", digestPrefix, sha256.Sum256(content))
}
