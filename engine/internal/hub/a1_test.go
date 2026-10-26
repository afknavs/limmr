package hub

import (
	"limmr/internal/model"
	"path/filepath"
	"testing"
)

// A1 deliberately retains the stored free/confirm rules: confirm now means
// consent in conversation. Nothing opts existing circles into Ask me in Limmr.
func TestA1ExistingRulesAndParkedDraftsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, rule := range []string{"free", "confirm", "never"} {
		if e = r.Put("circles", rule, model.Circle{ID: rule, SendRule: rule, Confirmed: true}); e != nil {
			t.Fatal(e)
		}
	}
	d := model.Draft{ID: "old-draft", State: "suggested", SuggestionID: "old-suggestion", FinalText: "Invented queued text", Rule: "confirm"}
	sg := model.Suggestion{ID: "old-suggestion", DraftID: d.ID, State: "pending", DraftText: d.FinalText}
	if e = r.Put("drafts", d.ID, d); e != nil {
		t.Fatal(e)
	}
	if e = r.Put("suggestions", sg.ID, sg); e != nil {
		t.Fatal(e)
	}
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		for _, rule := range []string{"free", "confirm", "never"} {
			var c model.Circle
			if e = r.Get("circles", rule, &c); e != nil || c.SendRule != rule {
				t.Fatal("rule changed on upgrade")
			}
		}
		var got model.Draft
		var suggestion model.Suggestion
		if e = r.Get("drafts", d.ID, &got); e != nil || got.State != "suggested" || got.FinalText != d.FinalText {
			t.Fatal("parked draft changed")
		}
		if e = r.Get("suggestions", sg.ID, &suggestion); e != nil || suggestion.State != "pending" || suggestion.DraftID != d.ID {
			t.Fatal("parked suggestion changed")
		}
		r.Close()
	}
}
