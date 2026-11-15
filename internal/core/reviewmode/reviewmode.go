// Package reviewmode owns the user's switch for receipt-driven development.
// The mode is decided by two files the user controls, one global and one per
// clone, and off always wins. Nothing in the repository's committed
// configuration can turn it on or off.
package reviewmode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CurrentSchemaVersion is the only setting file version this package reads.
const CurrentSchemaVersion = 1

// FileName is the setting file name in both scopes.
const FileName = "review-mode.json"

// Mode is the review mode a scope sets.
type Mode string

const (
	On  Mode = "on"
	Off Mode = "off"
)

// Scope names where a setting lives.
type Scope string

const (
	ScopeGlobal Scope = "global"
	ScopeClone  Scope = "clone"
)

// Source names what decided the effective mode.
type Source string

const (
	SourceDefault Source = "default"
	SourceGlobal  Source = "global"
	SourceClone   Source = "clone"
)

// Locations are the setting file paths for each scope.
type Locations struct {
	Global string
	Clone  string
}

// Path returns the setting file of one scope.
func (locations Locations) Path(scope Scope) string {
	if scope == ScopeGlobal {
		return locations.Global
	}
	return locations.Clone
}

// Status is the effective mode and how each scope contributed to it. A nil
// scope value means that scope is unset.
type Status struct {
	Effective Mode
	DecidedBy Source
	Global    *Mode
	Clone     *Mode
}

type setting struct {
	SchemaVersion int  `json:"schema_version"`
	Mode          Mode `json:"mode"`
}

// ParseMode accepts exactly "on" or "off".
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case On, Off:
		return Mode(value), nil
	}
	return "", fmt.Errorf("mode %q must be %q or %q", value, On, Off)
}

// ParseScope accepts exactly "global" or "clone".
func ParseScope(value string) (Scope, error) {
	switch Scope(value) {
	case ScopeGlobal, ScopeClone:
		return Scope(value), nil
	}
	return "", fmt.Errorf("scope %q must be %q or %q", value, ScopeClone, ScopeGlobal)
}

// Read resolves the effective mode without writing anything. A setting file
// that cannot be trusted is an error, never a silent "on".
func Read(locations Locations) (Status, error) {
	global, err := readSetting(locations.Global)
	if err != nil {
		return Status{}, err
	}
	clone, err := readSetting(locations.Clone)
	if err != nil {
		return Status{}, err
	}
	return resolve(global, clone), nil
}

// resolve applies "off wins": a clone off decides first, then a global off,
// then a clone on, then a global on. With nothing set the mode is on.
func resolve(global, clone *Mode) Status {
	status := Status{Effective: On, DecidedBy: SourceDefault, Global: global, Clone: clone}
	switch {
	case is(clone, Off):
		status.Effective, status.DecidedBy = Off, SourceClone
	case is(global, Off):
		status.Effective, status.DecidedBy = Off, SourceGlobal
	case is(clone, On):
		status.DecidedBy = SourceClone
	case is(global, On):
		status.DecidedBy = SourceGlobal
	}
	return status
}

func is(value *Mode, mode Mode) bool {
	return value != nil && *value == mode
}

func readSetting(path string) (*Mode, error) {
	if path == "" {
		return nil, nil
	}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read review mode setting %s: %w", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var parsed setting
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("invalid review mode setting %s: %w", path, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid review mode setting %s: trailing data after the setting", path)
	}
	if parsed.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("invalid review mode setting %s: schema_version %d is not supported", path, parsed.SchemaVersion)
	}
	mode, err := ParseMode(string(parsed.Mode))
	if err != nil {
		return nil, fmt.Errorf("invalid review mode setting %s: %w", path, err)
	}
	return &mode, nil
}

// Write stores mode at path atomically, so a reader sees either the previous
// setting or the new one, never a partial file.
func Write(path string, mode Mode) error {
	if _, err := ParseMode(string(mode)); err != nil {
		return err
	}
	encoded, err := json.Marshal(setting{SchemaVersion: CurrentSchemaVersion, Mode: mode})
	if err != nil {
		return fmt.Errorf("encode review mode setting: %w", err)
	}
	encoded = append(encoded, '\n')

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create review mode directory %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, FileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("write review mode setting %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write review mode setting %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write review mode setting %s: %w", path, err)
	}
	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return fmt.Errorf("write review mode setting %s: %w", path, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("write review mode setting %s: %w", path, err)
	}
	return nil
}
