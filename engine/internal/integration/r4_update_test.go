package integration_test

import (
	"bufio"
	"encoding/json"
	"io"
	"limmr/internal/health"
	"limmr/internal/wa"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type r4Probe struct {
	Code     int
	Version  string
	Messages int
}

// Subprocess fixture uses the production status handler and message store. It
// deliberately has no listener, WhatsApp client, app bundle or launchd access.
func TestR4UpdateServingProcess(t *testing.T) {
	mode := os.Getenv("LIMMR_R4_UPDATE_PROCESS")
	if mode == "" {
		return
	}
	r := newRig(t)
	handler := r.api.Handler()
	for {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/status", nil)
		req.Header.Set("Authorization", "Bearer "+r.api.StatusToken)
		handler.ServeHTTP(w, req)
		var st struct {
			Version string `json:"engine_version"`
		}
		if json.Unmarshal(w.Body.Bytes(), &st) != nil {
			os.Exit(20)
		}
		p := r4Probe{Code: w.Code, Version: st.Version}
		ms, err := r.m.Messages(wa.Query{Chat: chatA}, []string{chatA})
		if err != nil {
			os.Exit(21)
		}
		p.Messages = len(ms)
		if mode == "failed-health" {
			p.Code = 503
		}
		if json.NewEncoder(os.Stdout).Encode(p) != nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// This covers the engine/OS replacement boundary. BundleUpdateInstaller and
// UpdateTransaction are app-owned and require their own end-to-end proof.
func TestR4BinaryReplacementServingHandshakeAndRollback(t *testing.T) {
	home := t.TempDir()
	installed, staged, previous := filepath.Join(home, "engine"), filepath.Join(home, "candidate"), filepath.Join(home, "previous")
	copyBinary := func(path string) {
		in, err := os.Open(os.Args[0])
		must(t, err)
		defer in.Close()
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		must(t, err)
		_, err = io.Copy(out, in)
		must(t, err)
		must(t, out.Close())
	}
	copyBinary(installed)
	copyBinary(staged)
	startProcess := func(mode string) <-chan r4Probe {
		cmd := exec.Command(installed, "-test.run=^TestR4UpdateServingProcess$")
		cmd.Env = append(os.Environ(), "LIMMR_R4_UPDATE_PROCESS="+mode, "LIMMR_HOME="+home, "TMPDIR="+home)
		stdout, err := cmd.StdoutPipe()
		must(t, err)
		must(t, cmd.Start())
		done := make(chan struct{})
		out := make(chan r4Probe, 1)
		go func() {
			defer close(out)
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				var p r4Probe
				if json.Unmarshal(scanner.Bytes(), &p) != nil {
					continue
				}
				select {
				case out <- p:
				case <-done:
					return
				}
			}
		}()
		t.Cleanup(func() { close(done); _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return out
	}
	read := func(ch <-chan r4Probe) r4Probe {
		select {
		case p, ok := <-ch:
			if !ok {
				t.Fatal("owned helper exited")
			}
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("owned helper health deadline exceeded")
			return r4Probe{}
		}
	}
	old := startProcess("healthy")
	probe := read(old)
	if probe.Code != 200 || probe.Version != health.Version || probe.Messages != 3 {
		t.Fatal("initial version/cached-read handshake failed")
	}
	at := time.Now()
	must(t, os.Rename(installed, previous))
	must(t, os.Rename(staged, installed))
	for i := 0; i < 3; i++ {
		if p := read(old); p != probe {
			t.Fatal("running helper changed during disk replacement")
		}
	}
	candidate := startProcess("failed-health")
	if read(candidate).Code == 200 {
		t.Fatal("failed candidate accepted")
	}
	// The test supervisor rolls back only its own throwaway executable paths.
	must(t, os.Rename(installed, staged))
	must(t, os.Rename(previous, installed))
	for i := 0; i < 3; i++ {
		if read(old) != probe {
			t.Fatal("old helper stopped serving during rollback")
		}
	}
	restored := startProcess("healthy")
	if read(restored) != probe {
		t.Fatal("restored binary failed authenticated handshake")
	}
	t.Logf("owned executable replacement, failed-health rollback and restart %.3fms", float64(time.Since(at))/float64(time.Millisecond))
}
