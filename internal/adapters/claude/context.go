package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/koard-ai/koard/internal/core/agentrouter"
	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/review"
	"github.com/koard-ai/koard/internal/core/snapshot"
)

type item struct {
	Kind    string `json:"kind"`
	URI     string `json:"uri"`
	Digest  string `json:"digest"`
	Content string `json:"content"`
}

func digest(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
func admittedPath(uri string) (string, error) {
	p, err := snapshot.CanonicalRelativePath(uri)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(filepath.ToSlash(strings.ToLower(p)), "/") {
		if part == ".git" || part == ".ssh" || part == ".aws" || part == ".env" || strings.HasPrefix(part, ".env.") || strings.Contains(part, "credentials") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || strings.HasPrefix(part, "id_rsa") || strings.HasPrefix(part, "id_ed25519") {
			return "", errors.New("Claude context refuses credential-like paths")
		}
	}
	return p, nil
}
func textItem(ref handoff.Reference, b []byte) (item, error) {
	if len(b) > maxBundleBytes || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return item{}, errors.New("Claude context requires bounded UTF-8 text without NUL")
	}
	if strings.Contains(string(b), "PRIVATE KEY-----") {
		return item{}, errors.New("Claude context refuses private-key material")
	}
	return item{ref.Kind, ref.URI, digest(b), string(b)}, nil
}
func (a *Adapter) contextItems(ctx context.Context, refs []handoff.Reference) ([]item, error) {
	var items []item
	seen := map[string]string{}
	total := 0
	add := func(ref handoff.Reference) ([]byte, error) {
		p, err := admittedPath(ref.URI)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(filepath.Join(a.workspace, p))
		if err != nil {
			return nil, errors.New("Claude context file is unavailable")
		}
		rel, err := filepath.Rel(a.workspace, real)
		if err != nil {
			return nil, err
		}
		if _, err = admittedPath("repo://" + filepath.ToSlash(rel)); err != nil {
			return nil, err
		}
		b, err := snapshot.ReadReference(ctx, a.workspace, ref)
		if err != nil {
			return nil, err
		}
		i, err := textItem(ref, b)
		if err != nil {
			return nil, err
		}
		if previous, ok := seen[ref.URI]; ok {
			if previous != i.Digest {
				return nil, errors.New("conflicting Claude context digests")
			}
			return b, nil
		}
		if len(items) >= 64 {
			return nil, errors.New("Claude context exceeds 64 files")
		}
		total += len(b)
		if total > maxBundleBytes {
			return nil, errors.New("Claude context exceeds byte limit")
		}
		seen[ref.URI] = i.Digest
		items = append(items, i)
		return b, nil
	}
	for _, ref := range refs {
		b, err := add(ref)
		if err != nil {
			return nil, err
		}
		if ref.Kind == agentrouter.ResultReferenceKind {
			if ref.Digest == "" {
				return nil, errors.New("prior handoff result requires a digest")
			}
			r, err := handoff.DecodeResult(bytes.NewReader(b))
			if err != nil {
				return nil, errors.New("invalid prior handoff result")
			}
			// One typed hop only: no Markdown parsing, objective-path inference or walk.
			for _, child := range append(append([]handoff.Reference(nil), r.Artifacts...), r.Evidence...) {
				if _, err := add(child); err != nil {
					return nil, err
				}
			}
		}
	}
	return items, nil
}
func reviewItems(ctx context.Context, r review.Request, dir string) ([]item, error) {
	b, err := json.Marshal(r.Snapshot)
	if err != nil {
		return nil, err
	}
	if r.SchemaVersion != 1 || r.ID == "" || r.Attempt < 1 || r.Snapshot.SchemaVersion != 1 || r.Target.CandidateID != r.Snapshot.CandidateID || digest(b) != r.Target.SnapshotID {
		return nil, errors.New("review snapshot does not match target")
	}
	if len(r.Snapshot.Entries) > 64 {
		return nil, errors.New("Claude review exceeds 64 files")
	}
	var total int64
	for _, entry := range r.Snapshot.Entries {
		if _, err := admittedPath(entry.URI); err != nil {
			return nil, err
		}
		if entry.Size < 0 || entry.Size > maxBundleBytes {
			return nil, errors.New("Claude review entry exceeds byte limit")
		}
		total += entry.Size
	}
	if total > maxBundleBytes {
		return nil, errors.New("Claude review exceeds byte limit")
	}
	// Reuse the core's snapshot reconstruction and digest verification. Claude is
	// never launched in this tree: only its admitted text goes to stdin.
	tree, err := os.MkdirTemp("", "koard-claude-evidence-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tree)
	if err := snapshot.Materialize(dir, tree, r.Snapshot); err != nil {
		return nil, err
	}
	refs := make([]handoff.Reference, 0, len(r.Snapshot.Entries))
	for _, entry := range r.Snapshot.Entries {
		refs = append(refs, handoff.Reference{Kind: entry.Kind, URI: entry.URI, Digest: entry.Digest})
	}
	// Do not expand handoff-result entries during immutable review.
	var items []item
	for _, ref := range refs {
		data, err := snapshot.ReadReference(ctx, tree, ref)
		if err != nil {
			return nil, err
		}
		i, err := textItem(ref, data)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, nil
}
