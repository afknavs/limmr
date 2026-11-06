package remote

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
)

// ChatGPT lost a rotation response on 4 Nov 2026 and came back 13 minutes later
// with the previous refresh token; Limmr rejected it and the connector dropped.
func TestRotatedRefreshSurvivesLostResponse(t *testing.T) {
	r := newAuthlogRig(t, true)
	form := func(raw string) url.Values {
		return url.Values{"grant_type": {"refresh_token"}, "client_id": {r.clientID}, "refresh_token": {raw}}
	}
	refresh := func(raw string) (int, string) {
		w := serve(r.s, tokenReq(form(raw)))
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		next, _ := m["refresh_token"].(string)
		return w.Code, next
	}
	count := func() int {
		var n int
		if e := r.s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='oauth_tokens' AND json_extract(data,'$.kind')='refresh'").Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}

	code, lost := refresh(validRefresh) // response never reaches the client
	if code != 200 || lost == "" {
		t.Fatal("first rotation failed")
	}
	r.now = r.now.Add(13 * time.Minute)
	code, kept := refresh(validRefresh)
	if code != 200 || kept == "" || kept == lost {
		t.Fatalf("replay of the rotated token: status %d", code)
	}
	if !strings.Contains(r.buf.String(), "reason=refresh_replayed") {
		t.Fatal("replay not recorded in the log")
	}
	// Using the replacement retires the old token and the unused sibling.
	if code, _ = refresh(kept); code != 200 {
		t.Fatal("replacement refresh failed")
	}
	if code, _ = refresh(validRefresh); code != 400 {
		t.Fatal("old token accepted after its replacement was used")
	}
	if code, _ = refresh(lost); code != 400 {
		t.Fatal("unused sibling accepted after the chain moved on")
	}
	// kept (consumed, inside window) + newest; the expired fixture was pruned.
	if n := count(); n != 2 {
		t.Fatalf("refresh records = %d, want 2", n)
	}
}

func TestRotatedRefreshReplayWindowEnds(t *testing.T) {
	r := newAuthlogRig(t, true)
	var fixture tokenRecord
	if r.s.Hub.Repo.Get("oauth_tokens", dbutil.Digest(validRefresh), &fixture) != nil {
		t.Fatal("fixture missing")
	}
	fixture.ExpiresAt = r.now.Add(30 * 24 * time.Hour)
	if e := r.s.Hub.Repo.Put("oauth_tokens", dbutil.Digest(validRefresh), fixture); e != nil {
		t.Fatal(e)
	}
	f := url.Values{"grant_type": {"refresh_token"}, "client_id": {r.clientID}, "refresh_token": {validRefresh}}
	if serve(r.s, tokenReq(f)).Code != 200 {
		t.Fatal("rotation failed")
	}
	r.now = r.now.Add(refreshReplayWindow)
	if w := serve(r.s, tokenReq(f)); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("replay after the window: status %d", w.Code)
	}
	if !strings.Contains(r.buf.String(), "reason=refresh_replaced") {
		t.Fatal("expired replay not recorded")
	}
	// A rotated token is still a refresh record, never an access token.
	if _, e := r.s.Verify(t.Context(), validRefresh, nil); e == nil {
		t.Fatal("rotated refresh token verified as access")
	}
	var rec tokenRecord
	if r.s.Hub.Repo.Get("oauth_tokens", dbutil.Digest(validRefresh), &rec) != nil || rec.ConsumedAt == nil {
		t.Fatal("rotated token should stay marked consumed until pruned")
	}
}
