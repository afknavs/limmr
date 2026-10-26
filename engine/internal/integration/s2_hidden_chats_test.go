package integration_test

import (
	"limmr/internal/model"
	"limmr/internal/wa"
	"strings"
	"testing"
	"time"
)

// A hidden DM is a chat exclusion, never a sender exclusion in shared groups.
func TestS2HiddenDMKeepsSharedGroupReadsDigestAndUpdates(t *testing.T) {
	r := newRig(t)
	if r.a.Preset != "own" {
		t.Fatal("fixture must exercise an own AI")
	}
	s, clock, _ := setupWatches(t, r)
	standingFeed(t, r)
	must(t, r.h.Repo.Put("circles", "design", model.Circle{ID: "design", Name: "Design Club", Confirmed: true, Chats: []model.CircleChat{{ChatJID: groupA}}}))
	must(t, r.h.Repo.Put("circles", "maya-dm", model.Circle{ID: "maya-dm", Name: "Maya catch-up", Confirmed: true, Chats: []model.CircleChat{{ChatJID: chatA}}}))
	// Record both before hiding, then prove projection rechecks current chat visibility.
	incoming(t, r, s, "s2-dm", chatA, "S2_PRIVATE_DM")
	// incoming() normally uses the chat itself as sender; this fixture deliberately uses the DM sender in a group.
	m := model.Message{ID: "s2-group", ChatJID: groupA, Sender: chatA, Content: "S2_GROUP_VISIBLE", Timestamp: epoch}
	must(t, r.m.PutMessage(m))
	must(t, s.Handle(t.Context(), wa.StoredEvent{Message: m}))
	clock.advance(time.Minute)
	must(t, s.Engine.FlushDue(t.Context()))
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA}
	status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 200)
	if _, err := r.call(t, "list_messages", map[string]any{"chat_jid": chatA}); err != model.ErrNotFound {
		t.Fatalf("hidden DM read: %v", err)
	}
	out, err := r.call(t, "list_messages", map[string]any{"chat_jid": groupA})
	must(t, err)
	if !strings.Contains(encode(out), "S2_GROUP_VISIBLE") {
		t.Fatal("hidden DM removed group message from same sender")
	}
	for query, want := range map[string]bool{"S2_PRIVATE_DM": false, "S2_GROUP_VISIBLE": true} {
		search, searchErr := r.call(t, "search_messages", map[string]any{"query": query})
		must(t, searchErr)
		if strings.Contains(encode(search), query) != want {
			t.Fatalf("search visibility mismatch for %s", query)
		}
	}
	out, err = r.call(t, "get_digest", map[string]any{})
	must(t, err)
	if !strings.Contains(encode(out), "S2_GROUP_VISIBLE") || strings.Contains(encode(out), "S2_PRIVATE_DM") {
		t.Fatal("digest did not filter by chat")
	}
	page := updates(t, r, map[string]any{})
	found := false
	for _, item := range page.Items {
		if item.MessageID == "s2-dm" {
			t.Fatal("hidden DM update leaked")
		}
		if item.MessageID == "s2-group" && item.Text == "S2_GROUP_VISIBLE" {
			found = true
		}
	}
	if !found {
		t.Fatal("hidden DM removed shared-group update")
	}
	// Unhide the DM independently; the same own AI can read and digest it again.
	settings.HiddenChats = nil
	status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 200)
	out, err = r.call(t, "list_messages", map[string]any{"chat_jid": chatA})
	must(t, err)
	if !strings.Contains(encode(out), "S2_PRIVATE_DM") {
		t.Fatal("unhiding did not restore DM reads")
	}
	out, err = r.call(t, "get_digest", map[string]any{})
	must(t, err)
	if !strings.Contains(encode(out), "S2_PRIVATE_DM") || !strings.Contains(encode(out), "S2_GROUP_VISIBLE") {
		t.Fatal("unhiding did not restore DM digest alongside shared-group messages")
	}
	settings.HiddenChats = []string{chatA}
	settings.HiddenChats = append(settings.HiddenChats, groupA)
	must(t, r.h.SetSettings(settings))
	if _, err := r.call(t, "list_messages", map[string]any{"chat_jid": groupA}); err != model.ErrNotFound {
		t.Fatalf("hidden group read: %v", err)
	}
	out, err = r.call(t, "get_digest", map[string]any{})
	must(t, err)
	if strings.Contains(encode(out), "S2_GROUP_VISIBLE") {
		t.Fatal("hidden group digest leaked")
	}
	for _, item := range updates(t, r, map[string]any{}).Items {
		if item.MessageID == "s2-group" {
			t.Fatal("hidden group update leaked")
		}
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("read-only checks invoked messenger")
	}
}
