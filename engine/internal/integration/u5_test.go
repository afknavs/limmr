package integration_test

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"testing"
	"time"
)

func TestU5DashboardLastSyncTracksWhatsAppEventsNotDatabaseWrites(t *testing.T) {
	r := newRig(t)
	stats := func() hub.DashboardStats {
		t.Helper()
		w := r.request(t, "GET", "/v1/stats", r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var v hub.DashboardStats
		must(t, json.Unmarshal(w.Body.Bytes(), &v))
		return v
	}
	// The rig wrote historical messages, but no WhatsApp event has been received.
	if stats().LastSyncAt != nil {
		t.Fatal("database import presented as a WhatsApp event")
	}
	at := epoch.Add(-16 * time.Minute)
	r.link.Touch(at)
	if got := stats().LastSyncAt; got == nil || !got.Equal(at) {
		t.Fatal("event time missing")
	}
	// A receipt/storage update must not make the connection look freshly synced.
	must(t, r.m.PutMessage(model.Message{ID: "u5-storage-only", ChatJID: chatA, Timestamp: epoch}))
	if got := stats().LastSyncAt; got == nil || !got.Equal(at) {
		t.Fatal("storage update changed event time")
	}
	// New received events advance the footer, even without a new stored message.
	next := epoch.Add(-time.Second)
	r.link.Touch(next)
	if got := stats().LastSyncAt; got == nil || !got.Equal(next) {
		t.Fatal("new event did not advance last sync")
	}
	r.api.Link = nil
	if stats().LastSyncAt != nil {
		t.Fatal("missing connection reused ingestion time")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("dashboard sent a message")
	}
}
