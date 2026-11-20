package skillregistry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

var authoredName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var sections = []string{"Activation Contract", "Hard Rules", "Decision Gates", "Execution Steps", "Output Contract", "References"}

func parse(data []byte, fallback string, strict bool) (Skill, error) {
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		License     string `yaml:"license"`
		Metadata    struct {
			Author  string `yaml:"author"`
			Version string `yaml:"version"`
		} `yaml:"metadata"`
	}
	if !utf8.Valid(data) {
		return Skill{}, errors.New("skill must be UTF-8")
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return Skill{}, errors.New("YAML frontmatter is required")
	}
	end := 1
	for end < len(lines) && lines[end] != "---" {
		end++
	}
	if end == len(lines) {
		return Skill{}, errors.New("frontmatter closing delimiter is required")
	}
	decoder := yaml.NewDecoder(bytes.NewBufferString(strings.Join(lines[1:end], "\n")))
	if err := decoder.Decode(&meta); err != nil {
		return Skill{}, fmt.Errorf("invalid frontmatter: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Skill{}, errors.New("frontmatter must contain one YAML document")
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = fallback
	}
	if strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.TrimSpace(name) == "" {
		return Skill{}, errors.New("name must be nonempty and contain no path separators or control characters")
	}
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		return Skill{}, errors.New("description is required")
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if body == "" {
		return Skill{}, errors.New("instruction body is required")
	}
	if strict {
		if !authoredName.MatchString(meta.Name) || strings.TrimSpace(meta.License) == "" || strings.TrimSpace(meta.Metadata.Author) == "" || strings.TrimSpace(meta.Metadata.Version) == "" {
			return Skill{}, errors.New("Koard authoring requires a lowercase hyphenated name, license, metadata.author and metadata.version")
		}
		var headings []string
		fenced := false
		for _, line := range lines[end+1:] {
			if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
				fenced = !fenced
				continue
			}
			if !fenced && strings.HasPrefix(line, "## ") {
				headings = append(headings, strings.TrimSpace(line[3:]))
			}
		}
		if strings.Join(headings, "|") != strings.Join(sections, "|") {
			return Skill{}, errors.New("Koard authoring requires the six documented sections in order")
		}
	}
	return Skill{Name: name, Description: description}, nil
}
