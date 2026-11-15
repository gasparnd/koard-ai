// Package risk assesses a candidate snapshot into a passive, medium, or high
// tier with stable reason codes. Under Koard's rules, documentation is passive,
// code and configuration are medium, and shell, process, and sensitive-path
// signals are high. Every list is closed and lives here, never in repository
// configuration, so a candidate cannot lower its own tier.
package risk

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/koard-ai/koard/internal/core/snapshot"
)

// CurrentSchemaVersion is the version of the persisted assessment.
const CurrentSchemaVersion = 1

// Tier is the depth of scrutiny a candidate calls for.
type Tier string

const (
	Passive Tier = "passive"
	Medium  Tier = "medium"
	High    Tier = "high"
)

// Reason codes. They are stable so automation can react without parsing
// prose.
const (
	CodeNonExecutableOnly   = "non_executable_only"
	CodeConfigurationChange = "configuration_change"
	CodeExecutableChange    = "executable_change"
	CodeShellSource         = "shell_source"
	CodeProcessBoundary     = "process_boundary"
	CodeHotPath             = "hot_path"
	CodeAgentEscalation     = "agent_escalation"
	CodeUnassessable        = "unassessable"
)

// Reason explains one signal behind the tier.
type Reason struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Escalation raises a candidate to high by citing a high-risk item: 1 data,
// 2 security, 3 shared contracts, 4 concurrency, 5 delivery, 6 no test would
// catch a regression. It can never lower a tier.
type Escalation struct {
	Item   int
	Reason string
}

// Assessment is the persisted tier of one snapshot.
type Assessment struct {
	SchemaVersion int      `json:"schema_version"`
	SnapshotID    string   `json:"snapshot_id"`
	Tier          Tier     `json:"tier"`
	ReviewDue     bool     `json:"review_due"`
	Reasons       []Reason `json:"reasons"`
}

var (
	documentationExtensions = map[string]bool{".md": true, ".rst": true}
	configurationExtensions = map[string]bool{".json": true, ".yaml": true, ".yml": true, ".toml": true}
	configurationFiles      = map[string]bool{"go.mod": true, "go.sum": true, "Dockerfile": true, "Makefile": true}
	shellExtensions         = map[string]bool{".sh": true, ".bash": true, ".zsh": true}
	processMarkers          = [][]byte{[]byte(`"os/exec"`), []byte("os.StartProcess"), []byte("syscall.Exec")}
	hotPathStems            = []string{"auth", "secur", "secret", "credential", "permission", "sandbox"}
)

type signal struct {
	tier   Tier
	reason Reason
}

// Assess classifies the verified snapshot stored under dir with identity id.
// A snapshot that cannot be verified is high and unassessable rather than an
// error, so a failure never reads as low risk. Only an invalid escalation is
// an error.
func Assess(dir, id string, escalations []Escalation) (Assessment, error) {
	escalated := make([]signal, 0, len(escalations))
	for _, escalation := range escalations {
		if escalation.Item < 1 || escalation.Item > 6 {
			return Assessment{}, fmt.Errorf("escalation item %d must be between 1 and 6", escalation.Item)
		}
		if strings.TrimSpace(escalation.Reason) == "" {
			return Assessment{}, errors.New("escalation reason is required")
		}
		escalated = append(escalated, signal{High, Reason{
			Code:   CodeAgentEscalation,
			Detail: fmt.Sprintf("item %d: %s", escalation.Item, escalation.Reason),
		}})
	}

	signals, err := classifySnapshot(dir, id)
	if err != nil {
		signals = []signal{{High, Reason{Code: CodeUnassessable, Detail: err.Error()}}}
	}
	return settle(id, append(signals, escalated...)), nil
}

func classifySnapshot(dir, id string) ([]signal, error) {
	loaded, err := snapshot.Load(dir, id)
	if err != nil {
		return nil, err
	}
	var signals []signal
	for _, entry := range loaded.Manifest.Entries {
		content, err := snapshot.ReadEntry(dir, entry)
		if err != nil {
			return nil, err
		}
		signals = append(signals, classify(strings.TrimPrefix(entry.URI, snapshot.RepoScheme), content)...)
	}
	return signals, nil
}

// classify returns the high signals of one file when it has any, otherwise
// its single passive or medium signal.
func classify(name string, content []byte) []signal {
	extension := strings.ToLower(path.Ext(name))
	shebang := bytes.HasPrefix(content, []byte("#!"))

	var high []signal
	if shebang || containsAny(content, processMarkers) {
		high = append(high, signal{High, Reason{Code: CodeProcessBoundary, Path: name}})
	}
	if shebang || shellExtensions[extension] || strings.HasPrefix(name, ".github/workflows/") {
		high = append(high, signal{High, Reason{Code: CodeShellSource, Path: name}})
	}
	if hotPath(name) {
		high = append(high, signal{High, Reason{Code: CodeHotPath, Path: name}})
	}
	if len(high) > 0 {
		return high
	}

	switch {
	case documentationExtensions[extension]:
		return []signal{{Passive, Reason{Code: CodeNonExecutableOnly}}}
	case configurationFiles[path.Base(name)] || configurationExtensions[extension]:
		return []signal{{Medium, Reason{Code: CodeConfigurationChange, Path: name}}}
	default:
		return []signal{{Medium, Reason{Code: CodeExecutableChange, Path: name}}}
	}
}

func containsAny(content []byte, markers [][]byte) bool {
	for _, marker := range markers {
		if bytes.Contains(content, marker) {
			return true
		}
	}
	return false
}

func hotPath(name string) bool {
	for _, segment := range strings.Split(strings.ToLower(name), "/") {
		for _, stem := range hotPathStems {
			if strings.HasPrefix(segment, stem) {
				return true
			}
		}
	}
	return false
}

// settle keeps the highest tier and only the reasons of that tier, in order
// and without duplicates. No signal at all is passive.
func settle(id string, signals []signal) Assessment {
	tier := Passive
	for _, current := range signals {
		if rank(current.tier) > rank(tier) {
			tier = current.tier
		}
	}

	reasons := []Reason{}
	seen := map[Reason]bool{}
	for _, current := range signals {
		if current.tier != tier || seen[current.reason] {
			continue
		}
		seen[current.reason] = true
		reasons = append(reasons, current.reason)
	}
	if tier == Passive {
		reasons = []Reason{{Code: CodeNonExecutableOnly}}
	}
	return Assessment{
		SchemaVersion: CurrentSchemaVersion,
		SnapshotID:    id,
		Tier:          tier,
		ReviewDue:     tier == High,
		Reasons:       reasons,
	}
}

func rank(tier Tier) int {
	switch tier {
	case High:
		return 2
	case Medium:
		return 1
	}
	return 0
}
