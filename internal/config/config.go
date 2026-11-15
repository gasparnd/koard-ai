package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"
)

const CurrentSchemaVersion = 1

type Config struct {
	SchemaVersion int            `yaml:"schema_version"`
	Project       ProjectConfig  `yaml:"project"`
	Workflow      WorkflowConfig `yaml:"workflow"`
	Memory        MemoryConfig   `yaml:"memory"`
	Agents        AgentsConfig   `yaml:"agents"`
}

type ProjectConfig struct {
	Name string `yaml:"name"`
}

type WorkflowConfig struct {
	Default string `yaml:"default"`
}

type MemoryConfig struct {
	Provider string `yaml:"provider"`
}

type AgentsConfig struct {
	Planner  AgentConfig `yaml:"planner"`
	Executor AgentConfig `yaml:"executor"`
	Verifier AgentConfig `yaml:"verifier"`
}

type AgentConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	return Parse(file)
}

func Parse(reader io.Reader) (Config, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	var parsed Config
	if err := decoder.Decode(&parsed); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, errors.New("configuration is empty")
		}
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Config{}, errors.New("multiple YAML documents are not supported")
	} else if !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode trailing configuration: %w", err)
	}

	if err := parsed.Validate(); err != nil {
		return Config{}, err
	}

	return parsed, nil
}

func (config Config) Validate() error {
	if config.SchemaVersion == 0 {
		return errors.New("schema_version is required")
	}
	if config.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; supported version is %d", config.SchemaVersion, CurrentSchemaVersion)
	}

	required := []struct {
		name  string
		value string
	}{
		{name: "project.name", value: config.Project.Name},
		{name: "workflow.default", value: config.Workflow.Default},
		{name: "memory.provider", value: config.Memory.Provider},
		{name: "agents.planner.provider", value: config.Agents.Planner.Provider},
		{name: "agents.planner.model", value: config.Agents.Planner.Model},
		{name: "agents.executor.provider", value: config.Agents.Executor.Provider},
		{name: "agents.executor.model", value: config.Agents.Executor.Model},
		{name: "agents.verifier.provider", value: config.Agents.Verifier.Provider},
		{name: "agents.verifier.model", value: config.Agents.Verifier.Model},
	}

	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}

	return nil
}
