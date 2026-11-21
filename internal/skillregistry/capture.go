package skillregistry

import (
	"errors"
	"sort"
)

const MaxCatalogSkills = 128
const MaxCatalogBytes = 1024 * 1024

// Source contains the exact bytes validated while discovering a winning skill.
// It is separate from Report so list output remains metadata-only.
type Source struct {
	Skill   Skill  `json:"skill"`
	Content string `json:"content"`
}

// Capture freezes winning source bytes without reopening paths. Invalid or
// oversized catalogs return diagnostics but no usable partial source set.
func Capture(project, home string) (Report, []Source, error) {
	s := scanner{report: Report{Skills: []Skill{}, Diagnostics: []Diagnostic{}}, winners: map[string]string{}, capture: true}
	report, err := s.scan(project, home)
	if err != nil {
		return report, nil, err
	}
	if report.HasErrors() {
		return report, nil, errors.New("skill catalog discovery failed; inspect skill validate diagnostics")
	}
	sort.Slice(s.sources, func(i, j int) bool { return s.sources[i].Skill.Name < s.sources[j].Skill.Name })
	return report, s.sources, nil
}
