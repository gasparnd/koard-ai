// Package nativeintegration installs explicit host assets without replacing files.
package nativeintegration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Bundle struct {
	Files     map[string][]byte
	Conflicts []string
}

type Result struct {
	Path     string
	Existing bool
}

// Setup preflights the entire bundle. A late failure retains completed files so
// retrying the same bundle converges. It assumes no hostile concurrent parent
// directory replacement; it is not an OS sandbox or a configuration migration.
func Setup(workspace string, bundle Bundle, dry bool) ([]Result, error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, path := range bundle.Conflicts {
		if err := safePath(root, path); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
			return nil, fmt.Errorf("existing configuration %s; use manual adoption", path)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	paths := make([]string, 0, len(bundle.Files))
	for path := range bundle.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	plan := make([]Result, 0, len(paths))
	for _, path := range paths {
		if err := safePath(root, path); err != nil {
			return nil, err
		}
		existing, err := identical(filepath.Join(root, path), bundle.Files[path])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		plan = append(plan, Result{Path: path, Existing: existing})
	}
	if dry {
		return plan, nil
	}
	completed := make([]Result, 0, len(plan))
	for _, item := range plan {
		if err := safePath(root, item.Path); err != nil {
			return completed, err
		}
		path := filepath.Join(root, item.Path)
		existing, err := identical(path, bundle.Files[item.Path])
		if err != nil {
			return completed, err
		}
		item.Existing = existing
		if !existing {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return completed, err
			}
			if err := publish(path, bundle.Files[item.Path]); err != nil {
				return completed, err
			}
		}
		completed = append(completed, item)
	}
	return completed, nil
}

func safePath(root, path string) error {
	if path == "." || filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid asset path %q", path)
	}
	parent := root
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe asset parent for %s", path)
		}
	}
	return nil
}

func identical(path string, want []byte) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(want)) {
		return false, fmt.Errorf("destination conflicts; no files are overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(got, want) {
		return false, fmt.Errorf("destination conflicts; no files are overwritten")
	}
	return true, nil
}

func publish(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".koard-native-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o644); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Linking a complete same-directory file publishes without replacing a racer.
	return os.Link(f.Name(), path)
}
