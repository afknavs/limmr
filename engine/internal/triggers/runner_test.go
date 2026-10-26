package triggers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func processRequest(t *testing.T, kind string) RunRequest {
	t.Helper()
	dir := t.TempDir()
	return RunRequest{ID: "run-1", HandoffID: "handoff-1", Runner: RunnerConfig{Kind: kind, SessionID: "session-42", Workdir: dir}, Prompt: "Hello Maya\nChange the navigation\n"}
}
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake ai")
	must(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0700))
	return p
}
func TestRunnerArgv(t *testing.T) {
	req := processRequest(t, "codex")
	req.Runner.Workdir = "/tmp/a project; echo nope"
	codex := CodexRunner{}
	claude := ClaudeRunner{}
	if got, want := codex.Argv(req), []string{"codex", "-C", req.Runner.Workdir, "-s", "workspace-write", "-a", "never", "exec", "resume", "session-42", "-"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%q != %q", got, want)
	}
	if got, want := claude.Argv(req), []string{"claude", "-p", "--resume", "session-42", "--permission-mode", "acceptEdits", "--allowedTools", "Read,Edit,Write,MultiEdit,Glob,Grep,NotebookEdit,mcp__limmr__*"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%q != %q", got, want)
	}
}
func TestRunnerStdinEnvironmentAndPrivateLog(t *testing.T) {
	for _, kind := range []string{"codex", "claude_code"} {
		t.Run(kind, func(t *testing.T) {
			req := processRequest(t, kind)
			binary := fakeBinary(t, "printf 'ARG:%s\\n' \"$@\"\nprintf 'CWD:%s\\n' \"$PWD\"\nprintf 'HOME:%s\\n' \"$HOME\"\nprintf 'HANDOFF:%s\\n' \"$LIMMR_HANDOFF_ID\"\nprintf 'BLOCKED:%s:%s:%s\\n' \"$BASH_ENV\" \"$DYLD_INSERT_LIBRARIES\" \"$OPENAI_API_KEY\"\n/bin/cat\nprintf 'stderr captured\\n' >&2\n")
			options := ProcessOptions{Binary: binary, LogDir: filepath.Join(t.TempDir(), "logs"), Env: []string{"HOME=" + req.Runner.Workdir, "PATH=/usr/bin:/bin", "LANG=C", "BASH_ENV=discard-me", "DYLD_INSERT_LIBRARIES=discard-me", "OPENAI_API_KEY=synthetic-discard", "LIMMR_HANDOFF_ID=forged"}}
			var runner Runner
			var argv []string
			if kind == "codex" {
				r := CodexRunner{Options: options}
				runner = r
				argv = r.Argv(req)
			} else {
				r := ClaudeRunner{Options: options}
				runner = r
				argv = r.Argv(req)
			}
			result, err := runner.Run(testCtx, req)
			must(t, err)
			if result.ExitCode != 0 || result.TimedOut {
				t.Fatalf("%+v", result)
			}
			data, err := os.ReadFile(result.LogPath)
			must(t, err)
			output := string(data)
			for _, arg := range argv[1:] {
				if !strings.Contains(output, "ARG:"+arg+"\n") {
					t.Fatal("argv missing from subprocess")
				}
			}
			canonicalWorkdir, err := filepath.EvalSymlinks(req.Runner.Workdir)
			must(t, err)
			for _, want := range []string{req.Prompt, "CWD:" + canonicalWorkdir, "HOME:" + req.Runner.Workdir, "HANDOFF:handoff-1", "BLOCKED:::", "stderr captured"} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing %q in fake process log", want)
				}
			}
			if strings.Contains(output, "discard-me") || strings.Contains(output, "synthetic-discard") || strings.Contains(output, "forged") {
				t.Fatal("unapproved environment inherited")
			}
			st, err := os.Stat(result.LogPath)
			must(t, err)
			if st.Mode().Perm() != 0600 {
				t.Fatal("log permissions")
			}
			st, err = os.Stat(options.LogDir)
			must(t, err)
			if st.Mode().Perm() != 0700 {
				t.Fatal("log directory permissions")
			}
			if _, err = runner.Run(testCtx, req); err == nil {
				t.Fatal("existing per-run log overwritten")
			}
		})
	}
}
func TestEnvironmentAllowlist(t *testing.T) {
	env := allowedEnvironment([]string{"PATH=/bin", "HOME=/tmp/fake", "LANG=C", "LC_ALL=C", "LC_CTYPE=C", "TERM=dumb", "TMPDIR=/tmp", "HOME=/tmp/last", "SSH_AUTH_SOCK=discard", "SHELL=/bin/sh", "NODE_OPTIONS=discard", "malformed", "LIMMR_HANDOFF_ID=forged"}, "real-id")
	if len(env) != 8 {
		t.Fatalf("%q", env)
	}
	for _, v := range env {
		if strings.Contains(v, "discard") || strings.Contains(v, "forged") || v == "HOME=/tmp/fake" {
			t.Fatal("unfiltered or duplicate env")
		}
	}
}
func TestRunnerExitStatus(t *testing.T) {
	req := processRequest(t, "claude_code")
	r := ClaudeRunner{Options: ProcessOptions{Binary: fakeBinary(t, "printf 'fake failure\\n' >&2\nexit 7\n"), LogDir: t.TempDir()}}
	result, err := r.Run(testCtx, req)
	if err == nil || result.ExitCode != 7 || result.TimedOut {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
func TestRunnerTimeoutKillsProcess(t *testing.T) {
	req := processRequest(t, "codex")
	r := CodexRunner{Options: ProcessOptions{Binary: fakeBinary(t, "echo $$\nexec /bin/sleep 30\n"), LogDir: t.TempDir(), Timeout: 2 * time.Second}}
	started := time.Now()
	result, err := r.Run(testCtx, req)
	if err == nil || !result.TimedOut || result.ExitCode == 0 {
		t.Fatalf("%+v %v", result, err)
	}
	if time.Since(started) > 4*time.Second {
		t.Fatal("timeout did not bound execution")
	}
	data, err := os.ReadFile(result.LogPath)
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	must(t, err)
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("timed-out fake process survived")
	}
}
func TestRunnerCancellation(t *testing.T) {
	req := processRequest(t, "claude_code")
	r := ClaudeRunner{Options: ProcessOptions{Binary: fakeBinary(t, "exec /bin/sleep 30\n"), LogDir: t.TempDir()}}
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	result, err := r.Run(ctx, req)
	if !errors.Is(err, context.Canceled) || result.TimedOut {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestRunnerLogBound(t *testing.T) {
	req := processRequest(t, "codex")
	r := CodexRunner{Options: ProcessOptions{Binary: fakeBinary(t, "printf '1234567890'\nprintf 'abcdefghij' >&2\n"), LogDir: t.TempDir(), MaxLogBytes: 8}}
	result, err := r.Run(testCtx, req)
	must(t, err)
	st, err := os.Stat(result.LogPath)
	must(t, err)
	if st.Size() != 8 {
		t.Fatalf("log grew to %d", st.Size())
	}
}
func TestRunnerPathResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-ai")
	must(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700))
	req := processRequest(t, "codex")
	r := CodexRunner{Options: ProcessOptions{Binary: "custom-ai", LogDir: t.TempDir(), Env: []string{"PATH=.:" + dir}}}
	_, err := r.Run(testCtx, req)
	must(t, err)
	req.ID = "run-2"
	r.Options.Env = []string{"PATH=."}
	if _, err := r.Run(testCtx, req); err == nil {
		t.Fatal("resolved executable from parent PATH/current directory")
	}
}
func TestRunnerRejectsOptionInjection(t *testing.T) {
	req := processRequest(t, "codex")
	req.Runner.SessionID = "--dangerous"
	r := CodexRunner{Options: ProcessOptions{Binary: "/not/executed", LogDir: t.TempDir()}}
	if _, err := r.Run(testCtx, req); err == nil {
		t.Fatal("accepted session flag")
	}
	req.Runner.SessionID = "valid"
	req.ID = "../outside"
	if _, err := r.Run(testCtx, req); err == nil {
		t.Fatal("accepted log path traversal")
	}
}
func TestSessionRunnerDispatch(t *testing.T) {
	for _, kind := range []string{"codex", "claude_code", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			req := processRequest(t, kind)
			options := ProcessOptions{Binary: fakeBinary(t, "exit 0\n"), LogDir: t.TempDir()}
			r := SessionRunner{Codex: CodexRunner{Options: options}, Claude: ClaudeRunner{Options: options}}
			_, err := r.Run(testCtx, req)
			if (kind == "unknown") != (err != nil) {
				t.Fatal(err)
			}
		})
	}
}
