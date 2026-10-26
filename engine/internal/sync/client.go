package sync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Credentials are machine-local, never replicated or returned by the control API.
type Credentials struct {
	PeerURL      string `json:"peer_url"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}

func (s *Service) SaveCredentials(c Credentials) error {
	root, e := os.OpenRoot(s.Home)
	if e != nil {
		return e
	}
	defer root.Close()
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	f, e := root.OpenFile(".sync-oauth-new", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer root.Remove(".sync-oauth-new")
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return root.Rename(".sync-oauth-new", "sync-oauth.json")
}
func (s *Service) ReadCredentials() (Credentials, error) {
	var c Credentials
	name := filepath.Join(s.Home, "sync-oauth.json")
	fi, e := os.Lstat(name)
	if e != nil {
		return c, e
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
		return c, fmt.Errorf("sync credential file must have mode 0600")
	}
	b, e := os.ReadFile(name)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func ValidatePeer(raw string) (string, error) {
	u, e := url.Parse(strings.TrimRight(raw, "/"))
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.Contains(u.Path, "..") || u.Opaque != "" {
		return "", ErrInvalid
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return "", fmt.Errorf("peer must use HTTPS (HTTP is allowed only on loopback)")
	}
	return u.String(), nil
}
func oauthClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func oauthCall(ctx context.Context, method, uri, content string, b []byte, out any) (*http.Response, error) {
	r, e := http.NewRequestWithContext(ctx, method, uri, bytes.NewReader(b))
	if e != nil {
		return nil, ErrInvalid
	}
	if content != "" {
		r.Header.Set("Content-Type", content)
	}
	resp, e := oauthClient().Do(r)
	if e != nil {
		return nil, fmt.Errorf("pairing peer unavailable")
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if e != nil {
		return nil, ErrInvalid
	}
	if resp.StatusCode == 303 {
		return resp, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pairing returned HTTP %d", resp.StatusCode)
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return nil, ErrInvalid
	}
	return resp, nil
}

type tokenReply struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func (s *Service) RunOnce(ctx context.Context, peer string) error {
	c, e := s.ReadCredentials()
	if e != nil {
		return fmt.Errorf("pair first with sync pair --peer")
	}
	valid, e := ValidatePeer(c.PeerURL)
	if e != nil || valid != c.PeerURL || peer != "" && peer != c.PeerURL {
		return ErrInvalid
	}
	var token tokenReply
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {c.ClientID}, "refresh_token": {c.RefreshToken}, "resource": {c.PeerURL + "/mcp"}}
	if _, e = oauthCall(ctx, "POST", c.PeerURL+"/token", "application/x-www-form-urlencoded", []byte(form.Encode()), &token); e != nil {
		return e
	}
	if token.Access == "" || token.Refresh == "" {
		return ErrInvalid
	}
	c.RefreshToken = token.Refresh
	// Persist rotation before exchanging data; no raw credential enters hub.db.
	if e = s.SaveCredentials(c); e != nil {
		return e
	}
	return s.Exchange(ctx, HTTPTransport{PeerURL: c.PeerURL, Token: token.Access})
}
func (s *Service) PairOutbound(parent context.Context, peer string, printCode func(string)) error {
	// The owner has ten minutes to approve; the first exchange afterwards may take much longer on a large graph.
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	peer, e := ValidatePeer(peer)
	if e != nil {
		return e
	}
	var existing Peer
	_ = s.Repo.Get("settings", "sync_peer", &existing)
	if existing.EngineID != "" {
		return fmt.Errorf("already paired; unpair first")
	}
	redirect := "https://limmr.invalid/dot-callback"
	b, _ := json.Marshal(map[string]any{"client_name": "Your dot", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"})
	var registration struct {
		ID string `json:"client_id"`
	}
	if _, e = oauthCall(ctx, "POST", peer+"/register", "application/json", b, &registration); e != nil {
		return e
	}
	random := make([]byte, 32)
	if _, e = rand.Read(random); e != nil {
		return e
	}
	verifier := base64.RawURLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(verifier))
	stateBytes := make([]byte, 32)
	if _, e = rand.Read(stateBytes); e != nil {
		return e
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	q := url.Values{"client_id": {registration.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "resource": {peer + "/mcp"}, "scope": {"mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {state}, "sync": {"true"}, "peer_engine_id": {s.EngineID}}
	resp, e := oauthCall(ctx, "GET", peer+"/authorize?"+q.Encode(), "", nil, nil)
	if e != nil {
		return e
	}
	location, e := url.Parse(resp.Header.Get("Location"))
	if e != nil || location.Query().Get("tx") == "" {
		return ErrInvalid
	}
	tx := location.Query().Get("tx")
	printed := false
	for {
		var status struct{ State, Redirect, Code string }
		if _, e = oauthCall(ctx, "GET", peer+"/pair/status?tx="+url.QueryEscape(tx), "", nil, &status); e != nil {
			return e
		}
		if !printed && status.State == "waiting" {
			printCode(status.Code)
			printed = true
		}
		switch status.State {
		case "approved":
			u, e := url.Parse(status.Redirect)
			if e != nil || u.Query().Get("state") != state || u.Query().Get("code") == "" {
				return ErrInvalid
			}
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {registration.ID}, "redirect_uri": {redirect}, "code_verifier": {verifier}, "code": {u.Query().Get("code")}, "resource": {peer + "/mcp"}}
			var token tokenReply
			if _, e = oauthCall(ctx, "POST", peer+"/token", "application/x-www-form-urlencoded", []byte(form.Encode()), &token); e != nil {
				return e
			}
			if token.Access == "" || token.Refresh == "" {
				return ErrInvalid
			}
			t := HTTPTransport{PeerURL: peer, Token: token.Access}
			page, e := t.Pull(ctx, PullRequest{PeerEngineID: s.EngineID, SyncVersion: Version, Schema: Schema})
			if e != nil {
				return e
			}
			if e = s.Pair(page.EngineID, page.EngineID); e != nil {
				return e
			}
			if e = s.SaveCredentials(Credentials{peer, registration.ID, token.Refresh}); e != nil {
				return e
			}
			return s.Exchange(parent, t)
		case "waiting":
		default:
			return errors.New("pairing denied or expired")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("pairing cancelled or expired")
		case <-timer.C:
		}
	}
}
