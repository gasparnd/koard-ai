package opencode

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/koard-ai/koard/internal/application/nativeintegration"
)

//go:embed nativeassets
var nativeAssets embed.FS

// NativeBundle is separate from the restricted proposal execution adapter.
func NativeBundle() (nativeintegration.Bundle, error) {
	bundle := nativeintegration.Bundle{
		Files:     map[string][]byte{},
		Conflicts: []string{"opencode.json", "opencode.jsonc", filepath.Join(".opencode", "opencode.json")},
	}
	err := fs.WalkDir(nativeAssets, "nativeassets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := nativeAssets.ReadFile(path)
		if err != nil {
			return err
		}
		bundle.Files[filepath.Join(".opencode", filepath.FromSlash(strings.TrimPrefix(path, "nativeassets/")))] = data
		return nil
	})
	return bundle, err
}
