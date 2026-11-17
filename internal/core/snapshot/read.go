package snapshot

import (
	"context"
	"errors"

	"github.com/koard-ai/koard/internal/core/handoff"
)

// ReadReference exposes the same confinement and size checks used by Capture,
// without writing a snapshot. A supplied digest must match the bytes read.
func ReadReference(ctx context.Context, workspace string, ref handoff.Reference) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := RealWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	data, err := readArtifact(root, ref)
	if err != nil {
		return nil, err
	}
	if ref.Digest != "" && digestOf(data) != ref.Digest {
		return nil, errors.New("reference digest does not match repository bytes")
	}
	return data, ctx.Err()
}
