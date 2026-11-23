package opencode

import (
	"embed"
	"path/filepath"

	"github.com/koard-ai/koard/internal/application/nativeintegration"
)

// Separate embedding keeps the default, assets-only bundle unchanged.
//
//go:embed engramassets
var engramAssets embed.FS

func NativeEngramBundle() (nativeintegration.Bundle, error) {
	bundle, err := NativeBundle()
	if err != nil {
		return bundle, err
	}
	for source, target := range map[string]string{
		"engram.ts":     ".opencode/plugins/engram.ts",
		"LICENSE":       ".opencode/engram/LICENSE",
		"README.md":     ".opencode/engram/README.md",
		"opencode.json": ".opencode/opencode.jsonc",
	} {
		data, err := engramAssets.ReadFile("engramassets/" + source)
		if err != nil {
			return bundle, err
		}
		bundle.Files[filepath.FromSlash(target)] = data
	}
	return bundle, nil
}
