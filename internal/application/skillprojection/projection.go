// Package skillprojection exposes an existing project skill through a relative
// directory link. Runtime adapters choose discovery directories, not authority.
package skillprojection

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/koard-ai/koard/internal/skillregistry"
)

type Target interface {
	SkillDirectory() string
}

type Result struct {
	Source, Destination string
	Existing            bool
}

var portableName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func ValidName(name string) bool {
	return len(name) <= 64 && portableName.MatchString(name)
}

// Project checks the entire project catalog before writing. Dry-run performs
// the same checks but creates neither parent directories nor the final link.
func Project(workspace, name string, target Target, dryRun bool) (Result, error) {
	var result Result
	if !ValidName(name) || target == nil {
		return result, fmt.Errorf("invalid projection name or target")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	directory := filepath.FromSlash(target.SkillDirectory())
	if directory == "." || !filepath.IsLocal(directory) || filepath.Clean(directory) != directory {
		return result, fmt.Errorf("projection directory must be a clean local path")
	}
	report, err := skillregistry.Scan(root, "", false)
	if err != nil {
		return result, err
	}
	if report.HasErrors() {
		return result, fmt.Errorf("project skill catalog is invalid; run koard skill validate for diagnostics")
	}
	var source string
	for _, skill := range report.Skills {
		if skill.Name == name {
			source = filepath.Dir(skill.Path)
			break
		}
	}
	if source == "" {
		return result, fmt.Errorf("project skill %q not found; drafts and global skills are not projected", name)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return result, err
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return result, fmt.Errorf("skill source must remain inside the project")
	}
	// Linking the enclosing directory must preserve the source file identity.
	info, err := os.Lstat(filepath.Join(source, "SKILL.md"))
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("project the directory containing a regular SKILL.md, not a linked entry file")
	}
	parent := filepath.Join(root, directory)
	destination := filepath.Join(parent, name)
	if err := checkParents(root, parent); err != nil {
		return result, err
	}
	result = Result{Source: source, Destination: destination}
	if info, err := os.Lstat(destination); err == nil {
		if info.IsDir() && destination == source {
			result.Existing = true
			return result, nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(destination)
			if err == nil && real == source {
				result.Existing = true
				return result, nil
			}
		}
		return Result{}, fmt.Errorf("projection destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return Result{}, err
	}
	if inside, err := filepath.Rel(source, destination); err == nil && filepath.IsLocal(inside) {
		return Result{}, fmt.Errorf("projection cannot be nested inside its source")
	}
	link, err := filepath.Rel(parent, source)
	if err != nil {
		return Result{}, err
	}
	if dryRun {
		return result, nil
	}
	// Existing links were checked above. Recheck after creation; this remains a
	// trusted-workspace operation, not protection against hostile rename races.
	if err := os.MkdirAll(parent, 0755); err != nil {
		return Result{}, err
	}
	if err := checkParents(root, parent); err != nil {
		return Result{}, err
	}
	if err := os.Symlink(link, destination); err != nil {
		return Result{}, fmt.Errorf("create projection link: %w (no copy fallback)", err)
	}
	return result, nil
}

func checkParents(root, path string) error {
	if path == root {
		return nil
	}
	if err := checkParents(root, filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("projection parent must be a real directory: %s", path)
	}
	return nil
}
