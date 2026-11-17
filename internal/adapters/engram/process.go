// Package engram implements the memory port using an authenticated, session-owned
// Engram 3.7.2 process. It never connects to an ambient daemon or remote endpoint.
package engram

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/koard-ai/koard/internal/core/memory"
)

const requestTimeout = 10 * time.Second
const maxResponseBytes = 2 << 20

type Options struct {
	Executable string
	DataDir    string // Required absolute directory; tests must use isolated storage.
	Workspace  string
}

// Adapter serializes session operations and shutdown. A successful Start has
// already resolved and registered exactly one canonical project-owned session.
type Adapter struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	client   *http.Client
	endpoint string
	token    string
	project  memory.Project
	session  string
	closed   bool
	closeErr error
}

var _ memory.Store = (*Adapter)(nil)

type readiness struct {
	Protocol   string `json:"protocol"`
	Endpoint   string `json:"endpoint"`
	PID        int    `json:"pid"`
	InstanceID string `json:"instance_id"`
}

func Start(ctx context.Context, opts Options) (*Adapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(opts.DataDir) || !filepath.IsAbs(opts.Workspace) {
		return nil, errors.New("Engram requires absolute data and workspace directories")
	}
	workspace, err := filepath.EvalSymlinks(opts.Workspace)
	if err != nil {
		return nil, errors.New("Engram workspace is unavailable")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return nil, errors.New("Engram workspace must be a directory")
	}
	if opts.Executable == "" {
		opts.Executable = "engram"
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, opts.Executable, "serve", "--session-owned")
	cmd.Dir = workspace
	cmd.Env = childEnvironment(opts.DataDir, token)
	cmd.Stderr = io.Discard // Never echo provider diagnostics that might contain secrets.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, errors.New("start owned Engram process failed")
	}
	a := &Adapter{cmd: cmd, stdin: stdin, done: make(chan struct{}), token: token}
	go func() { _ = cmd.Wait(); close(a.done) }()
	startup, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	type readyResult struct {
		ready readiness
		err   error
	}
	readyCh := make(chan readyResult, 1)
	go func() {
		reader := bufio.NewReaderSize(stdout, 1025)
		line, err := reader.ReadSlice('\n')
		var ready readiness
		if err == nil && len(line) <= 1024 {
			err = decodeJSON(line, &ready, true)
		} else {
			err = errors.New("invalid Engram readiness frame")
		}
		readyCh <- readyResult{ready, err}
		_, _ = io.Copy(io.Discard, reader)
	}()
	fail := func(err error) (*Adapter, error) { _ = a.stop(); return nil, err }
	var ready readiness
	select {
	case result := <-readyCh:
		if result.err != nil {
			return fail(errors.New("invalid Engram readiness frame"))
		}
		ready = result.ready
	case <-startup.Done():
		return fail(errors.New("Engram startup deadline exceeded"))
	}
	if ready.Protocol != "engram-session-owned-v1" || ready.PID != cmd.Process.Pid || !lowerHex(ready.InstanceID, 16) || !loopbackEndpoint(ready.Endpoint) {
		return fail(errors.New("Engram readiness identity mismatch"))
	}
	a.endpoint = ready.Endpoint
	a.client = &http.Client{
		Timeout:       requestTimeout,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: (&net.Dialer{Timeout: requestTimeout}).DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Engram redirects are refused") },
	}
	if err := a.qualify(startup, ready.InstanceID); err != nil {
		return fail(err)
	}
	project, err := a.resolve(startup, workspace)
	if err != nil {
		return fail(err)
	}
	a.project = project
	id, err := randomHex(16)
	if err != nil {
		return fail(err)
	}
	id = "koard-" + id
	var ack struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	body := map[string]any{"id": id, "project": project.Name, "directory": workspace, "ownership_mode": "project_owned", "resume": false}
	if err := a.request(startup, http.MethodPost, "/sessions", body, 201, &ack); err != nil {
		return fail(err)
	}
	if ack.ID != id || ack.Status != "created" {
		return fail(errors.New("Engram session acknowledgement mismatch"))
	}
	a.session = ack.ID
	return a, nil
}

func childEnvironment(dataDir, token string) []string {
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		// Project discovery must not inherit Git repository redirection. Preserve
		// ordinary OS configuration, but never ambient Engram/cloud bindings.
		if strings.HasPrefix(strings.ToUpper(name), "ENGRAM_") || strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "ENGRAM_DATA_DIR="+dataDir, "ENGRAM_TOKEN="+token, "ENGRAM_CLOUD_AUTOSYNC=0")
}

func (a *Adapter) qualify(ctx context.Context, instance string) error {
	var health struct {
		Status       string `json:"status"`
		Service      string `json:"service"`
		Version      string `json:"version"`
		Instance     string `json:"instance_id"`
		Capabilities struct {
			Isolated bool `json:"isolated_session_registration"`
		} `json:"capabilities"`
	}
	if err := a.request(ctx, "GET", "/health", nil, 200, &health); err != nil {
		return err
	}
	if health.Status != "ok" || health.Service != "engram" || health.Version != "3.7.2" || health.Instance != instance || !health.Capabilities.Isolated {
		return errors.New("Engram 3.7.2 health identity or capabilities mismatch")
	}
	var compatibility struct {
		Revision   int    `json:"revision"`
		Source     string `json:"source"`
		Status     string `json:"status"`
		Active     bool   `json:"active_reuse_authorized"`
		Instance   string `json:"instance_id"`
		Runtime    string `json:"runtime_id"`
		Capability string `json:"capability"`
	}
	if err := a.request(ctx, "GET", "/compatibility", nil, 200, &compatibility); err != nil {
		return err
	}
	if compatibility.Revision != 1 || compatibility.Source != "active" || compatibility.Status != "ready" || !compatibility.Active || compatibility.Instance != instance || strings.TrimSpace(compatibility.Runtime) == "" || compatibility.Capability != "startup-verified-store-v1" {
		return errors.New("Engram active storage compatibility is unavailable")
	}
	return nil
}

// Close ends only the acknowledged session, then releases the owned child's
// stdin. An uncertain close is reported, never retried against another service.
func (a *Adapter) Close(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return a.closeErr
	}
	a.closed = true
	if a.session != "" {
		var ack struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		a.closeErr = a.request(ctx, "POST", "/sessions/"+url.PathEscape(a.session)+"/end", map[string]string{"summary": ""}, 200, &ack)
		if a.closeErr == nil && (ack.ID != a.session || ack.Status != "completed") {
			a.closeErr = errors.New("Engram session close acknowledgement mismatch")
		}
	}
	a.closeErr = errors.Join(a.closeErr, a.stop())
	return a.closeErr
}

func (a *Adapter) stop() error {
	_ = a.stdin.Close()
	if a.client != nil {
		a.client.CloseIdleConnections()
	}
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-a.done:
		return nil
	case <-timer.C:
		_ = a.cmd.Process.Kill()
		select {
		case <-a.done:
			return errors.New("Engram child required forced shutdown")
		case <-time.After(time.Second):
			return errors.New("Engram child shutdown could not be confirmed")
		}
	}
}

func (a *Adapter) available(project string) error {
	if a.closed || project != a.project.Name || project == "" {
		return errors.New("Engram session is closed or project does not match")
	}
	select {
	case <-a.done:
		return errors.New("owned Engram process exited")
	default:
		return nil
	}
}

func (a *Adapter) request(ctx context.Context, method, path string, body any, status int, output any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.endpoint+path, reader)
	if err != nil {
		return errors.New("invalid Engram request")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		return errors.New("Engram request failed; mutation outcome may be unknown")
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		return fmt.Errorf("Engram returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return errors.New("Engram response is unavailable or exceeds size limit")
	}
	if err := decodeJSON(data, output, false); err != nil {
		return errors.New("invalid Engram JSON response")
	}
	return nil
}

func decodeJSON(data []byte, output any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if strict {
		d.DisallowUnknownFields()
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("null response")
	}
	if err := d.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing response data")
	}
	return nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func lowerHex(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size && value == strings.ToLower(value)
}
func loopbackEndpoint(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}
