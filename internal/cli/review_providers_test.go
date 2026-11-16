package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koard-ai/koard/internal/adapters/codex"
	"github.com/koard-ai/koard/internal/adapters/opencode"
)

func TestReviewProvidersSelectsTheConfiguredRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		provider     string
		model        string
		expectedType string
	}{
		{name: "opencode", provider: "opencode", model: "openai/test-model", expectedType: "opencode"},
		{name: "codex", provider: "codex", model: "gpt-5-codex", expectedType: "codex"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			project := writeProviderProject(t, test.provider, test.model)

			reviewer, corrector, identity, err := reviewProviders(project)
			if err != nil {
				t.Fatalf("resolve providers: %v", err)
			}
			if identity.Runtime != test.provider {
				t.Fatalf("expected identity runtime %q, got %q", test.provider, identity.Runtime)
			}
			if identity.Model != test.model {
				t.Fatalf("expected identity model %q, got %q", test.model, identity.Model)
			}
			if corrector == nil {
				t.Fatal("expected a corrector runner for the correction lifecycle")
			}
			switch test.expectedType {
			case "opencode":
				if _, ok := reviewer.(*opencode.Adapter); !ok {
					t.Fatalf("expected *opencode.Adapter, got %T", reviewer)
				}
			case "codex":
				if _, ok := reviewer.(*codex.Adapter); !ok {
					t.Fatalf("expected *codex.Adapter, got %T", reviewer)
				}
			}
		})
	}
}

func TestReviewProvidersFailsClosedOnUnsupportedProvider(t *testing.T) {
	t.Parallel()

	project := writeProviderProject(t, "unset", "unset")

	_, _, _, err := reviewProviders(project)
	if err == nil {
		t.Fatal("expected an unsupported-provider error")
	}
	for _, expected := range []string{`"unset"`, `"opencode"`, `"codex"`} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error to name %s, got %v", expected, err)
		}
	}
}

func TestReviewProvidersRejectsMissingModel(t *testing.T) {
	t.Parallel()

	project := writeProviderProject(t, "codex", "")

	_, _, _, err := reviewProviders(project)
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("expected model validation error, got %v", err)
	}
}

// writeProviderProject creates the minimum project a provider selection reads:
// a versioned configuration naming one verifier provider and model.
func writeProviderProject(t *testing.T, provider, model string) string {
	t.Helper()

	project := t.TempDir()
	configDirectory := filepath.Join(project, ".koard")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	configuration := "schema_version: 1\n" +
		"project:\n  name: koard\n" +
		"workflow:\n  default: odd\n" +
		"memory:\n  provider: engram\n" +
		"agents:\n" +
		"  planner:\n    provider: unset\n    model: unset\n" +
		"  executor:\n    provider: unset\n    model: unset\n" +
		"  verifier:\n    provider: " + provider + "\n    model: " + model + "\n"
	if err := os.WriteFile(filepath.Join(configDirectory, "koard.yaml"), []byte(configuration), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}
