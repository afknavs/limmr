package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// EnvironmentAllowlist documents the inherited keys. No authentication-token,
// loader, shell startup, or provider/config override variables are inherited.
var EnvironmentAllowlist = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "TERM"}

func allowedEnvironment(source []string, id string) []string {
	// Keep policy internal so the exported documentation slice cannot broaden it.
	allowed := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TERM": true}
	values := map[string]string{}
	var order []string
	for _, entry := range source {
		k, v, ok := strings.Cut(entry, "=")
		if ok && allowed[k] {
			if _, exists := values[k]; !exists {
				order = append(order, k)
			}
			values[k] = v
		}
	}
	out := make([]string, 0, len(order)+1)
	for _, k := range order {
		out = append(out, k+"="+values[k])
	}
	return append(out, "LIMMR_HANDOFF_ID="+id)
}

// Binary is an administrator-selected executable. Tests use fake executables.
type ProcessOptions struct {
	Binary      string
	LogDir      string
	Timeout     time.Duration // default 15 minutes
	Env         []string      // nil means os.Environ; always filtered
	MaxLogBytes int64         // default 20 MiB
}
type CodexRunner struct{ Options ProcessOptions }
type ClaudeRunner struct{ Options ProcessOptions }
type SessionRunner struct {
	Codex  CodexRunner
	Claude ClaudeRunner
}

func (r SessionRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	switch req.Runner.Kind {
	case "codex":
		return r.Codex.Run(ctx, req)
	case "claude_code":
		return r.Claude.Run(ctx, req)
	default:
		return RunResult{ExitCode: -1}, errors.New("unsupported runner")
	}
}

// Argv puts -C in Codex's global option position, before the exec subcommand.
func (r CodexRunner) Argv(req RunRequest) []string {
	binary := r.Options.Binary
	if binary == "" {
		binary = "codex"
	}
	args := []string{binary, "-C", req.Runner.Workdir, "-s", "workspace-write", "-a", "never"}
	if req.MCPURL != "" {
		b, _ := json.Marshal(req.MCPURL)
		args = append(args, "-c", "mcp_servers.limmr.url="+string(b), "-c", "mcp_servers.limmr.http_headers={}", "-c", `mcp_servers.limmr.bearer_token_env_var="LIMMR_RUN_TOKEN"`)
	}
	return append(args, "exec", "resume", req.Runner.SessionID, "-")
}
func (r ClaudeRunner) Argv(req RunRequest) []string {
	binary := r.Options.Binary
	if binary == "" {
		binary = "claude"
	}
	args := []string{binary, "-p", "--resume", req.Runner.SessionID, "--permission-mode", "acceptEdits", "--allowedTools", "Read,Edit,Write,MultiEdit,Glob,Grep,NotebookEdit,mcp__limmr__*"}
	if req.MCPURL != "" {
		b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"limmr": map[string]any{"type": "http", "url": req.MCPURL, "headers": map[string]string{"Authorization": "Bearer ${LIMMR_RUN_TOKEN}"}}}})
		args = append(args, "--strict-mcp-config", "--mcp-config", string(b))
	}
	return args
}
func (r CodexRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.Runner.Kind != "codex" {
		return RunResult{ExitCode: -1}, errors.New("runner kind mismatch")
	}
	return runProcess(ctx, req, r.Options, r.Argv(req))
}
func (r ClaudeRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.Runner.Kind != "claude_code" {
		return RunResult{ExitCode: -1}, errors.New("runner kind mismatch")
	}
	return runProcess(ctx, req, r.Options, r.Argv(req))
}

type boundedLog struct {
	mu        sync.Mutex
	dst       io.Writer
	remaining int64
}

func (w *boundedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(p)
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	if len(p) > 0 {
		n, err := w.dst.Write(p)
		w.remaining -= int64(n)
		if err != nil {
			return n, err
		}
	}
	return original, nil
}
func runProcess(ctx context.Context, req RunRequest, o ProcessOptions, argv []string) (RunResult, error) {
	result := RunResult{ExitCode: -1}
	if !validSession(req.Runner.SessionID) || !validSession(req.ID) || !validSession(req.HandoffID) || !filepath.IsAbs(req.Runner.Workdir) {
		return result, errors.New("invalid runner request")
	}
	if o.LogDir == "" || !filepath.IsAbs(o.LogDir) {
		return result, errors.New("absolute per-run log directory required")
	}
	if o.Timeout == 0 {
		o.Timeout = 15 * time.Minute
	}
	if o.Timeout < 0 {
		return result, errors.New("invalid process timeout")
	}
	if o.MaxLogBytes <= 0 {
		o.MaxLogBytes = 20 << 20
	}
	if err := os.MkdirAll(o.LogDir, 0700); err != nil {
		return result, errors.New("cannot create runner log directory")
	}
	result.LogPath = filepath.Join(o.LogDir, req.ID+".log")
	f, err := os.OpenFile(result.LogPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, errors.New("cannot create private run log")
	}
	defer f.Close()
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	env = allowedEnvironment(env, req.HandoffID)
	if req.MCPToken != "" {
		env = append(env, "LIMMR_RUN_TOKEN="+req.MCPToken)
	}
	// Resolve using the filtered child's PATH, refusing relative PATH entries.
	binary := argv[0]
	if !filepath.IsAbs(binary) {
		if strings.ContainsRune(binary, os.PathSeparator) {
			return result, errors.New("runner binary must be absolute or a command name")
		}
		binary, err = resolveBinary(binary, env)
		if err != nil {
			return result, err
		}
	}
	runctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runctx, binary, argv[1:]...)
	cmd.Dir = req.Runner.Workdir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(req.Prompt)
	log := &boundedLog{dst: f, remaining: o.MaxLogBytes}
	safeLog := &credentialLog{dst: log, secret: []byte(req.MCPToken)}
	defer safeLog.Flush()
	cmd.Stdout = safeLog
	cmd.Stderr = safeLog
	// macOS and Linux: kill helpers as well as the parent on timeout/cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if cmd.Process != nil {
		// Also reap helpers that outlive an otherwise completed parent.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	result.TimedOut = errors.Is(runctx.Err(), context.DeadlineExceeded)
	if err != nil {
		if result.TimedOut {
			return result, errors.New("session runner timed out")
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("session runner failed; inspect private run log")
	}
	if err = f.Sync(); err != nil {
		return result, errors.New("cannot sync private run log")
	}
	return result, nil
}
func resolveBinary(name string, env []string) (string, error) {
	var path string
	for _, v := range env {
		if k, p, ok := strings.Cut(v, "="); ok && k == "PATH" {
			path = p
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		st, err := os.Stat(candidate)
		if err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("session executable unavailable in allowed PATH")
}
