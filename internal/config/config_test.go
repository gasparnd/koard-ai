package config

import (
	"strings"
	"testing"
)

func TestParseValidVersionOneConfiguration(t *testing.T) {
	t.Parallel()

	parsed, err := Parse(strings.NewReader(validConfiguration))
	if err != nil {
		t.Fatalf("expected valid configuration, got %v", err)
	}

	if parsed.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected schema version %d, got %d", CurrentSchemaVersion, parsed.SchemaVersion)
	}
	if parsed.Project.Name != "koard" {
		t.Errorf("expected project name koard, got %q", parsed.Project.Name)
	}
	if parsed.Agents.Executor.Provider != "codex" {
		t.Errorf("expected executor provider codex, got %q", parsed.Agents.Executor.Provider)
	}
}

func TestParseRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		expected      string
	}{
		{
			name:          "missing schema version",
			configuration: strings.Replace(validConfiguration, "schema_version: 1\n", "", 1),
			expected:      "schema_version is required",
		},
		{
			name:          "unsupported schema version",
			configuration: strings.Replace(validConfiguration, "schema_version: 1", "schema_version: 2", 1),
			expected:      "unsupported schema_version 2",
		},
		{
			name:          "unknown field",
			configuration: validConfiguration + "unknown: true\n",
			expected:      "field unknown not found",
		},
		{
			name:          "missing project name",
			configuration: strings.Replace(validConfiguration, "name: koard", "name: ''", 1),
			expected:      "project.name is required",
		},
		{
			name:          "missing role model",
			configuration: strings.Replace(validConfiguration, "model: gpt-5", "model: ''", 1),
			expected:      "agents.executor.model is required",
		},
		{
			name:          "multiple documents",
			configuration: validConfiguration + "---\nschema_version: 1\n",
			expected:      "multiple YAML documents are not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(strings.NewReader(test.configuration))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %q", test.expected, err)
			}
		})
	}
}

const validConfiguration = `schema_version: 1
project:
  name: koard
workflow:
  default: odd
memory:
  provider: engram
agents:
  planner:
    provider: claude
    model: sonnet
  executor:
    provider: codex
    model: gpt-5
  verifier:
    provider: opencode
    model: default
`
