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
