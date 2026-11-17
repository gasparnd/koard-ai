package engram

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/koard-ai/koard/internal/core/handoff"
	"github.com/koard-ai/koard/internal/core/memory"
)

func (a *Adapter) resolve(ctx context.Context, cwd string) (memory.Project, error) {
	var response struct {
		Project   string   `json:"project"`
		Source    string   `json:"project_source"`
		CWD       string   `json:"cwd"`
		Available []string `json:"available_projects"`
	}
	if err := a.request(ctx, "GET", "/project/current?"+url.Values{"cwd": {cwd}}.Encode(), nil, 200, &response); err != nil {
		return memory.Project{}, err
	}
	if response.CWD != cwd || response.Source == "ambiguous" || len(response.Available) > 0 || strings.TrimSpace(response.Project) == "" || strings.TrimSpace(response.Project) != response.Project {
		return memory.Project{}, errors.New("Engram canonical project is unresolved or ambiguous")
	}
	return memory.Project{Name: response.Project, Directory: cwd}, nil
}

func (a *Adapter) Resolve(ctx context.Context, cwd string) (memory.Project, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.available(a.project.Name); err != nil {
		return memory.Project{}, err
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil || !filepath.IsAbs(cwd) || canonical != a.project.Directory {
		return memory.Project{}, errors.New("Engram workspace does not match registered session")
	}
	project, err := a.resolve(ctx, canonical)
	if err != nil {
		return memory.Project{}, err
	}
	if project != a.project {
		return memory.Project{}, errors.New("Engram project changed since registration")
	}
	return project, nil
}

type observation struct {
	ID           int64   `json:"id"`
	Project      string  `json:"project"`
	Scope        string  `json:"scope"`
	Type         string  `json:"type"`
	Title        string  `json:"title"`
	Content      string  `json:"content"`
	ReviewAfter  *string `json:"review_after"`
	SupersededBy *string `json:"superseded_by_sync_id"`
	DeletedAt    *string `json:"deleted_at"`
}

func (o observation) owned(project string) bool {
	return o.ID > 0 && o.Project == project && o.Scope == "project" && o.DeletedAt == nil
}

func (a *Adapter) full(ctx context.Context, id int64) (memory.Observation, error) {
	var o observation
	if err := a.request(ctx, "GET", "/observations/"+strconv.FormatInt(id, 10), nil, 200, &o); err != nil {
		return memory.Observation{}, err
	}
	if !o.owned(a.project.Name) || o.ID != id || len(o.Content) > 50000 || len(o.Title) > 4096 || strings.TrimSpace(o.Title) == "" || strings.TrimSpace(o.Content) == "" {
		return memory.Observation{}, errors.New("Engram full observation ownership or payload mismatch")
	}
	state := "active"
	if o.ReviewAfter != nil && strings.TrimSpace(*o.ReviewAfter) != "" {
		var parsed time.Time
		var err error
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
			parsed, err = time.Parse(layout, *o.ReviewAfter)
			if err == nil {
				break
			}
		}
		if err != nil {
			return memory.Observation{}, errors.New("Engram observation has an invalid review timestamp")
		}
		if !parsed.After(time.Now()) {
			state = "needs_review"
		}
	}
	if o.SupersededBy != nil && *o.SupersededBy != "" {
		state = "needs_review"
	}
	return memory.Observation{ID: o.ID, Project: o.Project, Scope: o.Scope, Type: o.Type, Title: o.Title, Content: o.Content, State: state}, nil
}

func (a *Adapter) Search(ctx context.Context, project, query string) ([]memory.Observation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.available(project); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || len(query) > 4096 {
		return nil, errors.New("Engram search requires a bounded nonempty query")
	}
	params := url.Values{"q": {query}, "project": {project}, "scope": {"project"}, "limit": {"20"}, "match_mode": {"all"}}
	var hits []observation
	if err := a.request(ctx, "GET", "/search?"+params.Encode(), nil, 200, &hits); err != nil {
		return nil, err
	}
	if len(hits) > 20 {
		return nil, errors.New("Engram returned too many observations")
	}
	seen := make(map[int64]bool, len(hits))
	items := make([]memory.Observation, 0, len(hits))
	for _, hit := range hits {
		if !hit.owned(project) || seen[hit.ID] {
			return nil, errors.New("Engram search observation ownership mismatch")
		}
		seen[hit.ID] = true
		item, err := a.full(ctx, hit.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// Create performs one POST only. Any error, including a failed full read after
// the acknowledgement, is handed to the coordinator as an unknown write result.
func (a *Adapter) Create(ctx context.Context, project, operation string, p handoff.MemoryProposal) (memory.Observation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.available(project); err != nil {
		return memory.Observation{}, err
	}
	if operation == "" || len(operation) > 256 {
		return memory.Observation{}, errors.New("Engram operation identity is required and bounded")
	}
	var ack struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	body := map[string]any{"session_id": a.session, "project": project, "scope": "project", "type": p.Type, "title": p.Title, "content": p.Content, "operation_id": operation, "capture_prompt": false}
	if err := a.request(ctx, "POST", "/observations", body, 201, &ack); err != nil {
		return memory.Observation{}, err
	}
	if ack.ID <= 0 || ack.Status != "saved" {
		return memory.Observation{}, errors.New("Engram write acknowledgement mismatch; outcome unknown")
	}
	item, err := a.full(ctx, ack.ID)
	if err != nil {
		return memory.Observation{}, err
	}
	if item.Type != p.Type || item.Title != p.Title || item.Content != p.Content {
		return memory.Observation{}, errors.New("Engram saved payload mismatch; outcome unknown")
	}
	return item, nil
}
