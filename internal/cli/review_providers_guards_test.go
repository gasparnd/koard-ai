package cli

import (
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/adapters/codex"
)

// TestProviderDispatchGuards pins the two dispatch properties that matter for
// review authority: the selected adapter is the one that runs both roles, and
// an unknown provider yields no adapter at all.
func TestProviderDispatchGuards(t *testing.T) {
	t.Parallel()

	t.Run("codex", func(t *testing.T) {
		t.Parallel()

		project := writeProviderProject(t, "codex", "gpt-5-codex")
		reviewer, corrector, identity, err := reviewProviders(project)
		if err != nil {
			t.Fatalf("resolve providers: %v", err)
		}
		if _, ok := reviewer.(*codex.Adapter); !ok {
			t.Fatalf("reviewer is %T", reviewer)
		}
		if any(reviewer) != any(corrector) {
			t.Fatalf("corrector is %T, want the same adapter", corrector)
		}
		if identity.Runtime != "codex" || identity.Model != "gpt-5-codex" {
			t.Fatalf("unexpected identity: %#v", identity)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		project := writeProviderProject(t, "claude", "sonnet")
		reviewer, corrector, _, err := reviewProviders(project)
		if err == nil || reviewer != nil || corrector != nil {
			t.Fatalf("unknown provider did not fail closed: %v", err)
		}
		for _, want := range []string{`"claude"`, `"opencode"`, `"codex"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not name %s", err, want)
			}
		}
	})
}
