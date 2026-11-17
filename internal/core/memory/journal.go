package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Journal directories are application-owned local state, not model paths.
// Refuse symlink components and sync each new directory entry before use.
func ensureDirectory(path string) error {
	parent := filepath.Dir(path)
	if parent != path {
		if err := ensureDirectory(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() {
			return errors.New("memory journal path is not a directory")
		}
		// Another process may have just created this directory without syncing
		// its parent yet. An existing entry is not itself durability evidence.
		return syncDirectory(parent)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("memory journal directory changed during creation")
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func publishRecord(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".memory-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Atomic no-replace publication: only one process can own this decision or
	// attempt. This follows the existing consent/snapshot publication pattern.
	if err = os.Link(tmp, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func persistExact(path string, value any) error {
	err := publishRecord(path, value)
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	expected, err := json.Marshal(value)
	if err != nil {
		return err
	}
	actual, err := readBytes(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, actual) {
		return errors.New("memory journal conflicts with exact current record")
	}
	return syncDirectory(filepath.Dir(path))
}

func readBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, errors.New("invalid memory journal file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("memory journal file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("memory journal file exceeds limit")
	}
	return data, nil
}

func readRecord(path string, value any) (bool, error) {
	data, err := readBytes(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return false, err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("invalid trailing memory record: %v", err)
	}
	return true, nil
}
