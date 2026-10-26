package hub_test

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainSendMigrationPreservesAccessAttributionAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	put := func(bucket, id string, v any) {
		t.Helper()
		if err := r.Put(bucket, id, v); err != nil {
			t.Fatal(err)
		}
	}
	put("settings", "main", map[string]any{"owner": "Alex"})
	for _, id := range []string{"agent", "blocked", "given"} {
		p := map[string]any{"access": map[string]any{"circles": []string{"circle"}, "unsorted": false, "until": "2026-10-20T00:00:00Z"}, "chats": map[string]any{"mode": "only", "jids": []string{"demo-chat"}}, "request_send": id != "given", "voices": []string{"as_agent"}, "signature": "— {agent}, on behalf of {owner}", "signature_mode": "always"}
		if id == "blocked" {
			p["voices"] = []string{}
		}
		put("agents", id, map[string]any{"id": id, "name": "Demo AI", "preset": "given", "permissions": p, "enabled": true})
	}
	put("circles", "circle", map[string]any{"id": "circle", "send_rule": "confirm", "confirmed": true, "allow_as_me": false, "default_voice": "as_agent", "pinned": true, "chats": []any{map[string]any{"chat_jid": "demo-chat"}}})
	suffix := "— Demo AI, on behalf of Alex"
	for _, bucket := range []string{"drafts", "approvals", "suggestions"} {
		put(bucket, "pending", map[string]any{"agent_id": "agent", "created_by": "agent", "state": "pending", "voice": "as_agent", "signed": true, "signature": suffix, "text": "Body\n" + suffix, "draft_text": "Body\n" + suffix + "\n" + suffix, "final_text": "Body\n" + suffix, "variations": []string{"Body\n" + suffix, "Alternative"}, "key": "dedupe", "message_id": "outbound", "status": "queued"})
		put(bucket, "sent", map[string]any{"agent_id": "agent", "created_by": "agent", "state": "sent", "voice": "as_agent", "signed": true, "text": "Body\n" + suffix, "final_text": "Body\n" + suffix, "draft_text": "Body\n" + suffix, "key": "retained", "message_id": "outbound-sent", "status": "read", "read_count": 2})
	}
	put("drafts", "unsigned", map[string]any{"state": "draft", "agent_id": "agent", "voice": "as_me", "signed": false, "final_text": "Caller body\n" + suffix})
	put("proposals", "proposal", map[string]any{"circle": map[string]any{"send_rule": "never", "allow_as_me": false, "default_voice": "as_agent"}})
	_, err = r.DB.Exec(`INSERT INTO activity(at,data) VALUES('2026-10-18T00:00:00Z',?)`, `{"id":1,"at":"2026-10-18T00:00:00Z","agent_id":"agent","kind":"sent","voice":"as_agent","voice_reason":"old","count":1,"note":"Historical body"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.DB.Exec(`INSERT INTO send_authors VALUES('demo-chat','outbound','agent','Demo AI'); DELETE FROM hub_schema WHERE version=7`)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		r, err = hub.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"agent", "blocked", "given"} {
			var a model.Agent
			if err = r.Get("agents", id, &a); err != nil {
				t.Fatal(err)
			}
			if a.Permissions.RequestSend != (id == "agent") || a.Permissions.Chats.Mode != "only" || len(a.Permissions.Chats.JIDs) != 1 || a.Permissions.Access.Unsorted || a.Permissions.Access.Until == nil || len(a.Permissions.Access.Circles) != 1 {
				t.Fatal("access changed")
			}
			var raw string
			if err = r.DB.QueryRow("SELECT data FROM objects WHERE bucket='agents' AND id=?", id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"signature", "voices"} {
				if strings.Contains(raw, key) {
					t.Fatal("obsolete permission retained")
				}
			}
		}
		var c map[string]any
		if err = r.Get("circles", "circle", &c); err != nil {
			t.Fatal(err)
		}
		if c["send_rule"] != "confirm" || c["pinned"] != true || c["allow_as_me"] != nil || c["default_voice"] != nil {
			t.Fatal("circle changed")
		}
		for _, bucket := range []string{"drafts", "approvals", "suggestions"} {
			var d map[string]any
			if err = r.Get(bucket, "pending", &d); err != nil {
				t.Fatal(err)
			}
			if d["text"] != "Body" || d["final_text"] != "Body" || d["draft_text"] != "Body" || d["key"] != "dedupe" || d["message_id"] != "outbound" || d["voice"] != nil || d["signed"] != nil {
				t.Fatal("pending migration mismatch")
			}
			if err = r.Get(bucket, "sent", &d); err != nil {
				t.Fatal(err)
			}
			if d["text"] != "Body\n"+suffix || d["status"] != "read" || d["key"] != "retained" {
				t.Fatal("history changed")
			}
		}
		var d model.Draft
		if err = r.Get("drafts", "unsigned", &d); err != nil {
			t.Fatal(err)
		}
		if d.FinalText != "Caller body\n"+suffix {
			t.Fatal("caller text stripped")
		}
		var raw string
		if err = r.DB.QueryRow("SELECT data FROM activity WHERE id=1").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var activity model.Activity
		if err = json.Unmarshal([]byte(raw), &activity); err != nil {
			t.Fatal(err)
		}
		if activity.AgentID != "agent" || activity.Note != "Historical body" || !strings.Contains(raw, "voice_reason") {
			t.Fatal("historical activity changed")
		}
		var agent string
		if err = r.DB.QueryRow("SELECT agent_id FROM send_authors WHERE message_id='outbound'").Scan(&agent); err != nil {
			t.Fatal(err)
		}
		if agent != "agent" {
			t.Fatal("author lost")
		}
		if err = r.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlainSendMigrationFromLegacyApprovalQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		bucket, id string
		value      any
	}{
		{"settings", "main", map[string]any{"owner": "Alex"}},
		{"agents", "old-ai", map[string]any{"id": "old-ai", "name": "Demo AI", "permissions": map[string]any{"signature": "— {agent}, on behalf of {owner}", "voices": []string{"as_agent"}, "request_send": true}}},
		{"approvals", "old", map[string]any{"id": "old", "agent_id": "old-ai", "chat_jid": "demo-chat", "kind": "message", "voice": "as_agent", "text": "Body\n— Demo AI, on behalf of Alex", "state": "pending"}},
	} {
		if err = r.Put(x.bucket, x.id, x.value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = r.DB.Exec("DELETE FROM hub_schema WHERE version IN (2,7)"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var d model.Draft
	var sg model.Suggestion
	var ap model.Approval
	if err = r.Get("drafts", "old", &d); err != nil {
		t.Fatal(err)
	}
	if err = r.Get("suggestions", "legacy-old", &sg); err != nil {
		t.Fatal(err)
	}
	if err = r.Get("approvals", "old", &ap); err != nil {
		t.Fatal(err)
	}
	if d.FinalText != "Body" || sg.DraftText != "Body" || ap.Text != "Body" || d.State != "suggested" || ap.State != "suggested" {
		t.Fatal("old queue retained generated text or became sendable")
	}
}

func TestPlainSendMigrationKeepsNeverModeCallerTextAndDeniedLegacyGrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"empty", "null", "missing", "allowed"} {
		p := map[string]any{"signature": "Custom", "signature_mode": "never", "request_send": true}
		switch id {
		case "empty":
			p["voices"] = []string{}
		case "null":
			p["voices"] = nil
		case "allowed":
			p["voices"] = []string{"as_agent"}
		}
		if err = r.Put("agents", id, map[string]any{"id": id, "permissions": p}); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.Put("approvals", "caller", map[string]any{"agent_id": "allowed", "voice": "as_agent", "state": "pending", "text": "Caller\nCustom"}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DB.Exec("DELETE FROM hub_schema WHERE version=7"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, id := range []string{"empty", "null", "missing", "allowed"} {
		var a model.Agent
		if err = r.Get("agents", id, &a); err != nil {
			t.Fatal(err)
		}
		if a.Permissions.RequestSend != (id == "allowed") {
			t.Fatal("legacy grant widened")
		}
	}
	var ap model.Approval
	if err = r.Get("approvals", "caller", &ap); err != nil {
		t.Fatal(err)
	}
	if ap.Text != "Caller\nCustom" {
		t.Fatal("caller-authored closing removed")
	}
}
