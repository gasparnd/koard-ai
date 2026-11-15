// Package mutation owns the boundary between a proposal and a workspace
// change. Version 1 has no apply path, so every capability that could mutate
// the workspace, the repository history, or a release is refused by name.
package mutation

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// CapabilityRead is the only capability version 1 permits.
	CapabilityRead = "repository:read"

	CapabilityWorkspaceWrite = "workspace:write"
	CapabilityCommit         = "git:commit"
	CapabilityPush           = "git:push"
	CapabilityPullRequest    = "pull_request:open"
	CapabilityMerge          = "merge"
	CapabilityRelease        = "release"
)

// ErrBlocked identifies a refusal produced by this boundary, so callers can
// distinguish it from an ordinary failure.
var ErrBlocked = errors.New("koard: mutation is blocked")

// BlockedError names every capability that was refused. Unknown capabilities
// are refused too: the guard fails closed instead of tolerating a value it does
// not recognize.
type BlockedError struct {
	Capabilities []string
}

func (blocked *BlockedError) Error() string {
	quoted := make([]string, 0, len(blocked.Capabilities))
	for _, capability := range blocked.Capabilities {
		quoted = append(quoted, strconv.Quote(capability))
	}
	return fmt.Sprintf("%v: %s is not permitted before an apply path exists",
		ErrBlocked, strings.Join(quoted, ", "))
}

func (blocked *BlockedError) Unwrap() error {
	return ErrBlocked
}

// ReadOnlyCapabilities returns the complete permitted set as a fresh slice, so
// a caller cannot mutate the boundary by editing the result.
func ReadOnlyCapabilities() []string {
	return []string{CapabilityRead}
}

// RequireReadOnly refuses any capability outside the permitted set.
func RequireReadOnly(capabilities []string) error {
	var blocked []string
	for _, capability := range capabilities {
		if capability == CapabilityRead {
			continue
		}
		blocked = append(blocked, capability)
	}
	if len(blocked) == 0 {
		return nil
	}

	return &BlockedError{Capabilities: blocked}
}
