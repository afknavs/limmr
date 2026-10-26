package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestCirclesMigrationParksLegacyApprovalsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, e := hub.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	old := model.Approval{ID: "old-draft", AgentID: "invented-agent", ChatJID: "15550100001@s.whatsapp.net", Kind: "message", Text: "Exact signed preview", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), State: "pending"}
	if e = r.Put("approvals", old.ID, old); e != nil {
		t.Fatal(e)
	}
	if _, e = r.DB.Exec("DELETE FROM hub_schema WHERE version=2"); e != nil {
		t.Fatal(e)
	}
	r.Close()
	for i := 0; i < 2; i++ {
		r, e = hub.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		xs, e := hub.List[model.Suggestion](r, "suggestions")
		if e != nil {
			t.Fatal(e)
		}
		var d model.Draft
		if e = r.Get("drafts", old.ID, &d); e != nil {
			t.Fatal(e)
		}
		if len(xs) != 1 || xs[0].DraftText != old.Text || d.State != "suggested" || d.Attachment != nil {
			t.Fatal("migration duplicated/lost draft")
		}
		if e = r.Get("approvals", old.ID, &old); e != nil || old.State != "suggested" {
			t.Fatal("old approval remained actionable")
		}
		r.Close()
	}
}
