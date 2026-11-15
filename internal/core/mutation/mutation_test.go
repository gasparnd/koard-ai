package mutation

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestRequireReadOnlyAllowsTheReadCapability(t *testing.T) {
	t.Parallel()

	if err := RequireReadOnly([]string{CapabilityRead}); err != nil {
		t.Fatalf("the read capability must be allowed: %v", err)
	}
	if err := RequireReadOnly(nil); err != nil {
		t.Fatalf("an empty capability set must be allowed: %v", err)
	}
}

func TestRequireReadOnlyRejectsEveryNamedMutation(t *testing.T) {
	t.Parallel()

	denied := []string{
		CapabilityWorkspaceWrite,
		CapabilityCommit,
		CapabilityPush,
		CapabilityPullRequest,
		CapabilityMerge,
		CapabilityRelease,
	}

	for _, capability := range denied {
		t.Run(capability, func(t *testing.T) {
			t.Parallel()

			err := RequireReadOnly([]string{CapabilityRead, capability})
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("capability %q must be blocked, got %v", capability, err)
			}
			var blocked *BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("expected a typed block, got %T", err)
			}
			if !strings.Contains(blocked.Error(), capability) {
				t.Fatalf("block %q does not name %q", blocked.Error(), capability)
			}
		})
	}
}

func TestRequireReadOnlyFailsClosedOnUnknownCapability(t *testing.T) {
	t.Parallel()

	err := RequireReadOnly([]string{"repository:write-everything"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("an unknown capability must fail closed, got %v", err)
	}
}

func TestRequireReadOnlyReportsEveryBlockedCapability(t *testing.T) {
	t.Parallel()

	err := RequireReadOnly([]string{CapabilityCommit, CapabilityPush})
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected a typed block, got %T", err)
	}
	if len(blocked.Capabilities) != 2 {
		t.Fatalf("expected both blocked capabilities, got %#v", blocked.Capabilities)
	}
}

func TestRequireReadOnlyRefusesValuesMeantToLookLikeRead(t *testing.T) {
	t.Parallel()

	// The guard compares exact values, so none of these may be normalized into
	// the permitted capability.
	refused := []string{
		"",
		" ",
		" repository:read",
		"repository:read ",
		"repository:read\n",
		"REPOSITORY:READ",
		"repository:\u00a0read",
		"repository:read,git:push",
		"repository:readx",
	}

	for _, capability := range refused {
		t.Run(strconv.Quote(capability), func(t *testing.T) {
			t.Parallel()

			err := RequireReadOnly([]string{capability})
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("capability %q must be refused, got %v", capability, err)
			}
			var blocked *BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("expected a typed block, got %T", err)
			}
			if !strings.Contains(blocked.Error(), strconv.Quote(capability)) {
				t.Fatalf("block %q does not name %q", blocked.Error(), capability)
			}
		})
	}
}

func TestRequireReadOnlyDoesNotMutateItsInput(t *testing.T) {
	t.Parallel()

	capabilities := []string{CapabilityRead, CapabilityCommit}
	_ = RequireReadOnly(capabilities)
	if len(capabilities) != 2 || capabilities[0] != CapabilityRead || capabilities[1] != CapabilityCommit {
		t.Fatalf("guard mutated its input: %#v", capabilities)
	}
}

func TestReadOnlyCapabilitiesIsTheSingleAllowedSet(t *testing.T) {
	t.Parallel()

	capabilities := ReadOnlyCapabilities()
	if len(capabilities) != 1 || capabilities[0] != CapabilityRead {
		t.Fatalf("unexpected read-only set: %#v", capabilities)
	}

	capabilities[0] = CapabilityCommit
	if ReadOnlyCapabilities()[0] != CapabilityRead {
		t.Fatal("the allowed set must not be shared mutable state")
	}
}
