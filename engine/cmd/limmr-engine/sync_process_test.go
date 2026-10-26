package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	knowledgeSync "limmr/internal/sync"
	"limmr/internal/tasks"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func y7Port(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
func y7Call(t *testing.T, method, uri, token string, body any) []byte {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r, e := http.NewRequest(method, uri, bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	c := http.Client{Timeout: 10 * time.Second}
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal("local request failed")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("%s %s: HTTP %d", method, r.URL.Path, resp.StatusCode)
	}
	return b
}

type y7Process struct {
	cmd  *exec.Cmd
	done chan error
}

func (p *y7Process) stop() {
	if p.cmd == nil {
		return
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	p.cmd = nil
}
func y7Start(t *testing.T, bin, home string, port, remote int) (*y7Process, string) {
	t.Helper()
	cmd := exec.Command(bin, "--demo", "--home", home, "--port", strconv.Itoa(port), "--remote-port", strconv.Itoa(remote))
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	p := &y7Process{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(p.stop)
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		b, e := os.ReadFile(filepath.Join(home, "control-token"))
		if e == nil {
			token := strings.TrimSpace(string(b))
			r, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/v1/agents", port), nil)
			r.Header.Set("Authorization", "Bearer "+token)
			resp, e := http.DefaultClient.Do(r)
			if e == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return p, token
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("engine readiness timed out")
	return nil, ""
}

// Contract 10 uses actual binaries, persistent throwaway homes, a real loopback
// remote listener, OAuth+PKCE owner approval, and outbound-only dot exchanges.
func TestContract10RealProcessesOAuth(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "limmr-engine")
	build := exec.Command("go", "build", "-o", bin, ".")
	if e := build.Run(); e != nil {
		t.Fatal("engine build failed", e)
	}
	macHome, dotHome := t.TempDir(), t.TempDir()
	macPort, remotePort, dotPort, dotRemote := y7Port(t), y7Port(t), y7Port(t), y7Port(t)
	mac, macToken := y7Start(t, bin, macHome, macPort, remotePort)
	dot, dotToken := y7Start(t, bin, dotHome, dotPort, dotRemote)
	macURL := fmt.Sprintf("http://127.0.0.1:%d", macPort)
	dotURL := fmt.Sprintf("http://127.0.0.1:%d", dotPort)
	peer := fmt.Sprintf("http://127.0.0.1:%d", remotePort)
	var initial map[string]any
	json.Unmarshal(y7Call(t, "GET", dotURL+"/v1/sync", dotToken, nil), &initial)
	if initial["paired"] != false {
		t.Fatal("unpaired behavior changed")
	}
	y7Call(t, "POST", macURL+"/v1/remote", macToken, map[string]any{"enabled": true, "public_url": peer})
	pair := exec.Command(bin, "sync", "pair", "--home", dotHome, "--peer", peer)
	pair.Stdout = io.Discard
	pair.Stderr = io.Discard
	if e := pair.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if pair.ProcessState == nil {
			_ = pair.Process.Kill()
		}
	})
	code := ""
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var status struct {
			Pending []struct {
				Code string
				Sync bool
			}
		}
		json.Unmarshal(y7Call(t, "GET", macURL+"/v1/remote", macToken, nil), &status)
		for _, p := range status.Pending {
			if p.Sync {
				code = p.Code
			}
		}
		if code != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(code) != 6 {
		t.Fatal("OAuth approval code missing")
	}
	y7Call(t, "POST", macURL+"/v1/sync/pair", macToken, map[string]string{"code": code})
	if e := pair.Wait(); e != nil {
		t.Fatal("dot pairing failed", e)
	}
	fi, e := os.Stat(filepath.Join(dotHome, "sync-oauth.json"))
	if e != nil || fi.Mode().Perm() != 0600 {
		t.Fatal("refresh token mode is not 0600")
	}
	// Tokens carry sync only. Revoking that single capability returns 403 without
	// changing ordinary read/write/send permissions.
	var agents struct {
		Agents []struct {
			ID, Name    string
			Permissions map[string]any
		}
	}
	json.Unmarshal(y7Call(t, "GET", macURL+"/v1/agents", macToken, nil), &agents)
	var agentID string
	var permissions map[string]any
	for _, a := range agents.Agents {
		if a.Name == "Your dot" {
			agentID = a.ID
			permissions = a.Permissions
		}
	}
	if agentID == "" || permissions["sync"] != true {
		t.Fatal("missing sync grant")
	}
	for _, key := range []string{"read_messages", "search", "read_people", "write_people", "save_summaries", "download_media", "request_send"} {
		if permissions[key] != false {
			t.Fatal("sync widened permission", key)
		}
	}
	raw, e := os.ReadFile(filepath.Join(dotHome, "sync-oauth.json"))
	if e != nil {
		t.Fatal(e)
	}
	var creds knowledgeSync.Credentials
	json.Unmarshal(raw, &creds)
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {creds.ClientID}, "refresh_token": {creds.RefreshToken}}
	resp, e := http.PostForm(peer+"/token", form)
	if e != nil {
		t.Fatal(e)
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	e = json.NewDecoder(resp.Body).Decode(&tokens)
	resp.Body.Close()
	if e != nil || tokens.Access == "" {
		t.Fatal("token refresh failed")
	}
	creds.RefreshToken = tokens.Refresh
	b, _ := json.Marshal(creds)
	if e = os.WriteFile(filepath.Join(dotHome, "sync-oauth.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	permissions["sync"] = false
	y7Call(t, "PATCH", macURL+"/v1/agents/"+agentID, macToken, map[string]any{"permissions": permissions})
	for _, path := range []string{"/sync/pull", "/sync/push", "/sync/blob"} {
		r, _ := http.NewRequest("POST", peer+path, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer "+tokens.Access)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("missing sync grant: %s HTTP %d", path, resp.StatusCode)
		}
	}
	permissions["sync"] = true
	y7Call(t, "PATCH", macURL+"/v1/agents/"+agentID, macToken, map[string]any{"permissions": permissions})
	// Both engines edit while offline. Give one photo only to the unreachable dot.
	mac.stop()
	dot.stop()
	var pngBytes bytes.Buffer
	_ = png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	photo := pngBytes.Bytes()
	sum := sha256.Sum256(photo)
	hash := hex.EncodeToString(sum[:])
	for i, home := range []string{macHome, dotHome} {
		repo, e := hub.Open(filepath.Join(home, "hub.db"))
		if e != nil {
			t.Fatal(e)
		}
		id := fmt.Sprintf("y7-person-%d", i)
		person := map[string]any{"id": id, "name": []string{"Maya Vale", "Riya Lane"}[i], "label": "Edited offline", "jids": []string{}}
		if i == 1 {
			person["photo"] = map[string]any{"sha256": hash, "updated_at": "2026-10-17T12:00:00Z"}
			os.MkdirAll(filepath.Join(home, "avatars/custom"), 0700)
			if e = os.WriteFile(filepath.Join(home, "avatars/custom", hash), photo, 0600); e != nil {
				t.Fatal(e)
			}
		}
		if e = repo.Put("people", id, person); e != nil {
			t.Fatal(e)
		}
		task := tasks.Task{ID: fmt.Sprintf("y7-task-%d", i), Title: []string{"Prepare workshop", "Read chapter"}[i], Kind: "long", Status: "active", LifespanDays: 365, Chats: []tasks.Entity{}, People: []tasks.Entity{}, Checklist: []tasks.Step{}, Items: []tasks.Item{}, UpdatesSent: []tasks.SentUpdate{}, CreatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
		if _, e = repo.DB.Exec(`INSERT INTO tasks VALUES(?,?)`, task.ID, dbutil.JSON(task)); e != nil {
			t.Fatal(e)
		}
		// Same due slot on both engines; startup tick may park it only on the holder.
		due := task
		due.ID = "y7-lease-slot"
		due.Title = "Workshop check-in"
		due.Chats = []tasks.Entity{{ID: "15550000001@s.whatsapp.net", Name: "Maya"}}
		due.CheckIn = &tasks.CheckIn{Rule: "once", AtTime: "12:00", To: due.Chats[0].ID, NextAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
		if _, e = repo.DB.Exec(`INSERT INTO tasks VALUES(?,?)`, due.ID, dbutil.JSON(due)); e != nil {
			t.Fatal(e)
		}
		repo.Close()
	}
	mac, macToken = y7Start(t, bin, macHome, macPort, remotePort)
	dot, dotToken = y7Start(t, bin, dotHome, dotPort, dotRemote)
	// The runtime runs immediately after readiness. Await its observable claim.
	deadline = time.Now().Add(10 * time.Second)
	count := func(home string) int {
		repo, e := hub.Open(filepath.Join(home, "hub.db"))
		if e != nil {
			t.Fatal(e)
		}
		defer repo.Close()
		var n int
		e = repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')='y7-lease-slot' AND json_extract(data,'$.state')='pending'`).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		return n
	}
	for time.Now().Before(deadline) && count(macHome) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if count(macHome) != 1 || count(dotHome) != 0 {
		t.Fatal("only the lease holder must park a check-in")
	}
	once := exec.Command(bin, "sync", "--once", "--home", dotHome, "--peer", peer)
	out, e := once.CombinedOutput()
	if e != nil {
		t.Fatal("scheduled outbound sync failed", e)
	}
	if !strings.Contains(string(out), "Sync complete") {
		t.Fatal("missing success status")
	}
	for _, path := range []string{"/v1/graph/export", "/v1/tasks?limit=500"} {
		decode := func(b []byte) any {
			var v any
			if e = json.Unmarshal(b, &v); e != nil {
				t.Fatal(e)
			}
			if m, ok := v.(map[string]any); ok && m["tasks"] != nil {
				xs := m["tasks"].([]any)
				sort.Slice(xs, func(i, j int) bool {
					return xs[i].(map[string]any)["id"].(string) < xs[j].(map[string]any)["id"].(string)
				})
			}
			return v
		}
		a := decode(y7Call(t, "GET", macURL+path, macToken, nil))
		b := decode(y7Call(t, "GET", dotURL+path, dotToken, nil))
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("knowledge differs after one scheduled exchange: %s", path)
		}
	}
	got, e := os.ReadFile(filepath.Join(macHome, "avatars/custom", hash))
	if e != nil || !bytes.Equal(got, photo) {
		t.Fatal("outbound-only photo missing on Mac")
	}
	// Restart the short command again: durable cursor and rotated token survive.
	once = exec.Command(bin, "sync", "--once", "--home", dotHome, "--peer", peer)
	if e = once.Run(); e != nil {
		t.Fatal("restart sync failed", e)
	}
	y7Call(t, "DELETE", macURL+"/v1/sync/pair", macToken, nil)
	y7Call(t, "DELETE", dotURL+"/v1/sync/pair", dotToken, nil)
	if _, e = os.Stat(filepath.Join(dotHome, "sync-oauth.json")); !os.IsNotExist(e) {
		t.Fatal("unpair retained refresh credential")
	}
	for _, base := range []struct{ URL, Token string }{{macURL, macToken}, {dotURL, dotToken}} {
		var status map[string]any
		json.Unmarshal(y7Call(t, "GET", base.URL+"/v1/sync", base.Token, nil), &status)
		if status["paired"] != false {
			t.Fatal("unpair did not clear pairing")
		}
	}
}
