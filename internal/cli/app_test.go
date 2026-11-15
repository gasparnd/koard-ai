package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesPortableProjectStructure(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"init"}, directory, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", exitCode, stderr.String())
	}
	for _, path := range append(projectDirectories, ".koard/koard.yaml") {
		if _, err := os.Stat(filepath.Join(directory, path)); err != nil {
			t.Errorf("expected %s to exist: %v", path, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(directory, ".koard", "koard.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"default: odd", "provider: engram", "planner:", "executor:", "verifier:"} {
		if !strings.Contains(string(config), expected) {
			t.Errorf("expected configuration to contain %q", expected)
		}
	}
}

func TestInitPreservesExistingConfiguration(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	configDirectory := filepath.Join(directory, ".koard")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDirectory, "koard.yaml")
	if err := os.WriteFile(configPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run([]string{"init"}, directory, &bytes.Buffer{}, &bytes.Buffer{})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != "custom: true\n" {
		t.Fatalf("existing configuration was overwritten: %q", config)
	}
}

func TestDoctorReportsMissingConfiguration(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run([]string{"doctor"}, t.TempDir(), &bytes.Buffer{}, &stderr)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "run 'koard init'") {
		t.Fatalf("expected actionable error, got %q", stderr.String())
	}
}

func TestDoctorValidatesConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		configuration  string
		expectedCode   int
		expectedOutput string
	}{
		{
			name:           "valid version one configuration",
			configuration:  defaultConfig,
			expectedCode:   0,
			expectedOutput: "schema version 1",
		},
		{
			name:           "unsupported schema version",
			configuration:  strings.Replace(defaultConfig, "schema_version: 1", "schema_version: 2", 1),
			expectedCode:   1,
			expectedOutput: "unsupported schema_version 2",
		},
		{
			name:           "unknown configuration field",
			configuration:  defaultConfig + "unknown: true\n",
			expectedCode:   1,
			expectedOutput: "field unknown not found",
		},
		{
			name:           "empty project name",
			configuration:  strings.Replace(defaultConfig, "name: koard-project", "name: '   '", 1),
			expectedCode:   1,
			expectedOutput: "project.name is required",
		},
		{
			name:           "empty workflow",
			configuration:  strings.Replace(defaultConfig, "default: odd", "default: ''", 1),
			expectedCode:   1,
			expectedOutput: "workflow.default is required",
		},
		{
			name:           "empty memory provider",
			configuration:  strings.Replace(defaultConfig, "provider: engram", "provider: ''", 1),
			expectedCode:   1,
			expectedOutput: "memory.provider is required",
		},
		{
			name:           "empty planner provider",
			configuration:  strings.Replace(defaultConfig, "planner:\n    provider: unset", "planner:\n    provider: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.planner.provider is required",
		},
		{
			name:           "empty planner model",
			configuration:  strings.Replace(defaultConfig, "planner:\n    provider: unset\n    model: unset", "planner:\n    provider: unset\n    model: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.planner.model is required",
		},
		{
			name:           "empty executor provider",
			configuration:  strings.Replace(defaultConfig, "executor:\n    provider: unset", "executor:\n    provider: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.executor.provider is required",
		},
		{
			name:           "empty executor model",
			configuration:  strings.Replace(defaultConfig, "executor:\n    provider: unset\n    model: unset", "executor:\n    provider: unset\n    model: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.executor.model is required",
		},
		{
			name:           "empty verifier provider",
			configuration:  strings.Replace(defaultConfig, "verifier:\n    provider: unset", "verifier:\n    provider: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.verifier.provider is required",
		},
		{
			name:           "empty verifier model",
			configuration:  strings.Replace(defaultConfig, "verifier:\n    provider: unset\n    model: unset", "verifier:\n    provider: unset\n    model: ''", 1),
			expectedCode:   1,
			expectedOutput: "agents.verifier.model is required",
		},
		{
			name:           "multiple documents",
			configuration:  defaultConfig + "---\n",
			expectedCode:   1,
			expectedOutput: "multiple YAML documents are not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			configDirectory := filepath.Join(directory, ".koard")
			if err := os.MkdirAll(configDirectory, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDirectory, "koard.yaml"), []byte(test.configuration), 0o644); err != nil {
				t.Fatal(err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := Run([]string{"doctor"}, directory, &stdout, &stderr)
			output := stdout.String() + stderr.String()

			if exitCode != test.expectedCode {
				t.Fatalf("expected exit code %d, got %d; output: %s", test.expectedCode, exitCode, output)
			}
			if !strings.Contains(output, test.expectedOutput) {
				t.Fatalf("expected output containing %q, got %q", test.expectedOutput, output)
			}
		})
	}
}

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run([]string{"unknown"}, t.TempDir(), &bytes.Buffer{}, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected unknown command error, got %q", stderr.String())
	}
}
