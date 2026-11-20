// Package skillregistry discovers instruction files without loading them into a
// runtime. It does not invoke providers, execute instructions, or publish a cache.
package skillregistry

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const MaxSkillBytes = 64 * 1024
const maxDirectoryEntries = 4096

// Ordering is part of discovery precedence, not provider selection.
var projectRoots = []string{"skills", ".koard/skills", ".opencode/skills", ".claude/skills", ".gemini/skills", ".trae/skills", ".cursor/skills", ".github/skills", ".codex/skills", ".qwen/skills", ".kiro/skills", ".openclaw/skills", ".pi/skills", ".agent/skills", ".agents/skills", ".atl/skills", ".hermes/skills"}
var globalRoots = []string{".koard/skills", ".pi/agent/skills", ".config/agents/skills", ".agents/skills", ".kimi-code/skills", ".kimi/skills", ".config/opencode/skills", ".config/kilo/skills", ".claude/skills", ".gemini/skills", ".gemini/config/skills", ".gemini/antigravity/skills", ".cursor/skills", ".copilot/skills", ".codex/skills", ".codeium/windsurf/skills", ".qwen/skills", ".kiro/skills", ".openclaw/skills", ".hermes/skills", ".trae/skills"}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type Report struct {
	Skills      []Skill      `json:"skills"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (r Report) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

type root struct{ path, allowed, scope string }
type scanner struct {
	report  Report
	roots   []root
	winners map[string]string
	strict  bool
	capture bool
	sources []Source
	bytes   int
}

// Scan uses project roots only unless home is explicitly supplied by the caller.
// All candidates are validated, including duplicates hidden by earlier roots.
func Scan(project, home string, strict bool) (Report, error) {
	s := scanner{report: Report{Skills: []Skill{}, Diagnostics: []Diagnostic{}}, winners: map[string]string{}, strict: strict}
	return s.scan(project, home)
}

func (s *scanner) scan(project, home string) (Report, error) {
	for _, group := range []struct {
		base, scope string
		paths       []string
	}{{project, "project", projectRoots}, {home, "global", globalRoots}} {
		if group.base == "" {
			continue
		}
		base, err := filepath.Abs(group.base)
		if err != nil {
			return s.report, err
		}
		real, err := filepath.EvalSymlinks(base)
		if err != nil {
			return s.report, fmt.Errorf("resolve %s directory: %w", group.scope, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return s.report, err
		}
		if !info.IsDir() {
			return s.report, fmt.Errorf("%s directory is not a directory", group.scope)
		}
		for _, path := range group.paths {
			s.roots = append(s.roots, root{filepath.Join(base, path), filepath.Join(real, path), group.scope})
		}
	}
	for _, r := range s.roots {
		s.scanRoot(r)
	}
	sort.Slice(s.report.Skills, func(i, j int) bool { return s.report.Skills[i].Name < s.report.Skills[j].Name })
	return s.report, nil
}

func (s *scanner) issue(code, path string, err error) {
	s.report.Diagnostics = append(s.report.Diagnostics, Diagnostic{"error", code, path, err.Error()})
}

func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !hasParentPrefix(rel)
}

func hasParentPrefix(path string) bool {
	return len(path) > 3 && path[:3] == ".."+string(filepath.Separator)
}

func (s *scanner) resolve(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, r := range s.roots {
		if within(r.allowed, real) {
			return real, nil
		}
	}
	return "", errors.New("symlink target is outside selected roots; use --global for supported global roots")
}

func (s *scanner) directory(path string) ([]os.DirEntry, error) {
	real, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("skill discovery root or category must be a directory")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.IsDir() || !os.SameFile(info, opened) {
		return nil, errors.New("skill directory changed while opening")
	}
	entries, err := f.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxDirectoryEntries {
		return nil, errors.New("directory entry limit exceeded")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (s *scanner) scanRoot(r root) {
	// Lstat distinguishes absent roots from dangling symlinks, which are errors.
	if _, err := os.Lstat(r.path); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		s.issue("root", r.path, err)
		return
	}
	entries, err := s.directory(r.path)
	if err != nil {
		s.issue("root", r.path, err)
		return
	}
	var categories []string
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(r.path, e.Name())
		if !s.candidate(path, r.scope) {
			categories = append(categories, path)
		}
	}
	// Direct skills take precedence over one category level within each root.
	for _, path := range categories {
		children, err := s.directory(path)
		if err != nil {
			s.issue("directory", path, err)
			continue
		}
		for _, child := range children {
			if child.IsDir() || child.Type()&os.ModeSymlink != 0 {
				s.candidate(filepath.Join(path, child.Name()), r.scope)
			}
		}
	}
}

// candidate reports presence even on invalid input: never descend into a skill.
func (s *scanner) candidate(dir, scope string) bool {
	if _, err := s.resolve(dir); err != nil {
		s.issue("directory", dir, err)
		return true
	}
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false
	} else if err != nil {
		s.issue("file", path, err)
		return true
	}
	data, err := s.read(path)
	if err != nil {
		s.issue("file", path, err)
		return true
	}
	skill, err := parse(data, filepath.Base(dir), s.strict)
	if err != nil {
		s.issue("validation", path, err)
		return true
	}
	skill.Path, skill.Scope = path, scope
	if winner, exists := s.winners[skill.Name]; exists {
		s.report.Diagnostics = append(s.report.Diagnostics, Diagnostic{"warning", "shadowed", path, fmt.Sprintf("skill %q is shadowed by %s", skill.Name, winner)})
		return true
	}
	s.winners[skill.Name] = path
	s.report.Skills = append(s.report.Skills, skill)
	if s.capture {
		if len(s.sources) >= MaxCatalogSkills || s.bytes+len(data) > MaxCatalogBytes {
			s.issue("catalog_limit", path, errors.New("skill catalog exceeds 128 skills or 1 MiB"))
		} else {
			s.sources = append(s.sources, Source{Skill: skill, Content: string(data)})
			s.bytes += len(data)
		}
	}
	return true
}

func (s *scanner) read(path string) ([]byte, error) {
	real, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("SKILL.md must be a regular file")
	}
	if info.Size() > MaxSkillBytes {
		return nil, errors.New("skill size exceeds 64 KiB")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("skill changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSkillBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSkillBytes {
		return nil, errors.New("skill size exceeds 64 KiB")
	}
	return data, nil
}
