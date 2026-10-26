package efforts

import (
	"encoding/json"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
)

func TestFilenameTitle(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"5. Review Article_Specific Findings.docx", "Review article specific findings"},
		{"01_AI-research__Review.pdf", "AI research review"},
		{"003 - NASA Mission_Plan.PDF", "NASA mission plan"},
		{"02_UNESCO Research_Review.docx", "UNESCO research review"},
		{"02_REVIEW_ARTICLE_SPECIFIC_FINDINGS.pdf", "Review article specific findings"},
		{"1. café__MRI-study.txt", "Café MRI study"},
		{"5. Review Article_Specific Experimental Results And Discussion.docx", "Review article specific experimental"},
		{"07-" + strings.Repeat("a", 41) + ".pdf", ""},
		{"/invented/03_Design-Club Notes.md", "Design club notes"},
	} {
		if got := FilenameTitle(tc.input); got != tc.want {
			t.Errorf("%q: %q != %q", tc.input, got, tc.want)
		}
	}
}

func TestNamingQueueScopePayloadAndRevision(t *testing.T) {
	s := qualitySeed(t, []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"})
	xs, total, err := s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if total != 1 || len(xs) != 1 || len(xs[0].Snippets) != 3 || xs[0].ParticipantFirstNames[0] != "Maya" {
		t.Fatal("owner naming context missing")
	}
	if !strings.HasPrefix(xs[0].Snippets[0], "Maya:") || !strings.HasPrefix(xs[0].Snippets[1], "You:") {
		t.Fatal("snippets lack first-name speaker attribution")
	}
	candidate := xs[0]
	// An unrelated activity changes the snapshot cache, but not the effort revision.
	testkit.Must(t, s.Hub.Record(model.Activity{AgentID: "fixture", Tool: "whoami", Kind: "read"}))
	same, _, err := s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if same[0].Revision != candidate.Revision {
		t.Fatal("activity invalidated naming cache")
	}
	a := model.Agent{ID: "fixture", Name: "Fixture AI", Permissions: model.Defaults(false)}
	remote, _, err := s.Unnamed(&a, 10)
	testkit.Must(t, err)
	data, _ := json.Marshal(remote)
	if len(remote) != 1 || len(remote[0].TopTerms) == 0 || strings.Contains(string(data), "snippets") || strings.Contains(string(data), "Maya") || strings.Contains(string(data), "budget is for") {
		t.Fatal("unsafe agent queue payload")
	}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	remote, total, err = s.Unnamed(&a, 10)
	testkit.Must(t, err)
	if len(remote) != 0 || total != 0 {
		t.Fatal("inaccessible queue count leaked")
	}
	// Evidence editing in place with the same timestamp must invalidate a pending result.
	var m model.Message
	testkit.Must(t, s.Hub.Messages.DB.QueryRow("SELECT id,chat_jid,sender,content,timestamp,is_from_me FROM messages WHERE id='quality-0'").Scan(&m.ID, &m.ChatJID, &m.Sender, &m.Content, &m.Timestamp, &m.IsFromMe))
	m.Content = "budget is for proposal revised"
	testkit.Must(t, s.Hub.Messages.PutMessage(m))
	title, summary := "Festival budget", "Maya requested a proposal review."
	v, err := s.Update(nil, candidate.ID, Update{Title: &title, Summary: &summary, OnlyUnnamed: true, ExpectedRevision: candidate.Revision})
	testkit.Must(t, err)
	if v.Title != "" {
		t.Fatal("stale naming applied")
	}
	xs, _, err = s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if xs[0].Revision == candidate.Revision {
		t.Fatal("evidence revision did not change")
	}
	v, err = s.Update(nil, candidate.ID, Update{Title: &title, Summary: &summary, OnlyUnnamed: true, ExpectedRevision: xs[0].Revision})
	testkit.Must(t, err)
	if v.Title != title || v.CreatedBy != "This Mac" {
		t.Fatal("fresh naming failed")
	}
	_, total, err = s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if total != 0 {
		t.Fatal("named effort remained queued")
	}
}

func TestNamingQueueRequiresEveryChatAndHonoursHidden(t *testing.T) {
	s := qualitySeed(t, []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"})
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "group-evidence", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "budget for the proposal", Timestamp: testkit.Epoch}))
	owner, _, err := s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if len(owner) != 1 || owner[0].ChatCount != 2 {
		t.Fatal("mixed-chat fixture missing")
	}
	a := model.Agent{ID: "limited", Name: "Fixture AI", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
	xs, total, err := s.Unnamed(&a, 10)
	testkit.Must(t, err)
	if total != 0 || len(xs) != 0 {
		t.Fatal("partially visible effort queued")
	}
	_, err = s.Update(&a, owner[0].ID, Update{Title: ptr("Private projection")})
	if err != model.ErrNotFound {
		t.Fatal("agent named partial effort")
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Phone, testkit.Group}
	testkit.Must(t, s.Hub.SetSettings(settings))
	_, total, err = s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if total != 0 {
		t.Fatal("hidden chat queued")
	}
}

func TestNamingContextBoundsAndConditionalOwnerEdits(t *testing.T) {
	s := qualitySeed(t, []string{"budget is for proposal", "budget for the proposal", "budget and a proposal", "budget is for proposal", "budget for the proposal", "budget and a proposal", "budget is for proposal", "budget for the proposal", "budget and a proposal"})
	xs, _, err := s.Unnamed(nil, 10)
	testkit.Must(t, err)
	if len(xs) != 1 || len(xs[0].Snippets) != 6 {
		t.Fatal("snippet count is not bounded at six")
	}
	for _, snippet := range xs[0].Snippets {
		if len([]rune(snippet)) > 160 {
			t.Fatal("snippet too long")
		}
	}
	candidate := xs[0]
	title := "Owner's purpose"
	_, err = s.Update(nil, candidate.ID, Update{Title: &title})
	testkit.Must(t, err)
	generated := "Generated purpose"
	v, err := s.Update(nil, candidate.ID, Update{Title: &generated, OnlyUnnamed: true, ExpectedRevision: candidate.Revision})
	testkit.Must(t, err)
	if v.Title != title || v.CreatedBy != "you" {
		t.Fatal("local result replaced owner edit")
	}
}

func TestDocumentReplyGetsCleanInferredTitle(t *testing.T) {
	s := qualitySeed(t, nil)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "article", ChatJID: testkit.Phone, Sender: testkit.Phone, MediaType: "document", Filename: "5. Review Article_Specific Findings.docx", Timestamp: testkit.Epoch.AddDate(0, 0, -2)}))
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "article-reply", ChatJID: testkit.Phone, IsFromMe: true, QuotedMessageID: "article", Content: "Can you review?", Timestamp: testkit.Epoch.AddDate(0, 0, -1)}))
	xs, err := s.List(nil, Filter{})
	testkit.Must(t, err)
	if len(xs) != 1 || xs[0].Title != "Review article specific findings" {
		t.Fatal("document inference did not use title cleaner")
	}
}
