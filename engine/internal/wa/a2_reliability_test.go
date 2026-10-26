package wa

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"limmr/internal/model"
)

func TestA2RestartMidHistorySyncAndReplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LIMMR_HOME", home)
	path := filepath.Join(home, "messages.db")
	s, err := OpenStore(path)
	check(t, err)
	l := offlineLive(t)
	l.Messages = s
	l.event(&events.HistorySync{Data: &waHistorySync.HistorySync{Progress: proto.Uint32(40)}})
	check(t, s.Batch(func(tx *Store) error {
		if err := tx.PutChat(model.Chat{JID: phone, Name: "Maya Rao"}); err != nil {
			return err
		}
		return tx.PutMessage(model.Message{ID: "committed-chunk", ChatJID: phone, Content: "Invented history", Timestamp: time.Now()})
	}))
	sentinel := errors.New("interrupted chunk")
	if err = s.Batch(func(tx *Store) error {
		if err := tx.PutMessage(model.Message{ID: "partial-chunk", ChatJID: phone, Content: "Invented partial", Timestamp: time.Now()}); err != nil {
			return err
		}
		return sentinel
	}); err != sentinel {
		t.Fatal("interruption not returned")
	}
	check(t, s.Close())
	s, err = OpenStore(path)
	check(t, err)
	defer s.Close()
	// Constructor only; never connect, pair or resume a WhatsApp client.
	recovered, err := NewLive(home, s, nil)
	check(t, err)
	defer recovered.Close()
	if recovered.SyncDone() {
		t.Fatal("partial sync became complete after restart")
	}
	ms, err := s.Messages(Query{Chat: phone}, []string{phone})
	check(t, err)
	if len(ms) != 1 || ms[0].ID != "committed-chunk" {
		t.Fatal("history transaction recovery lost atomicity")
	}
	msg := model.Message{ID: "committed-chunk", ChatJID: phone, Content: "Invented history", Timestamp: ms[0].Timestamp}
	check(t, s.PutMessage(msg))
	check(t, s.PutMessage(msg))
	recovered.event(&events.HistorySync{Data: &waHistorySync.HistorySync{Progress: proto.Uint32(100)}})
	if !recovered.SyncDone() {
		t.Fatal("sync completion not persisted")
	}
	check(t, recovered.Close())
	again, err := NewLive(home, s, nil)
	check(t, err)
	defer again.Close()
	if !again.SyncDone() {
		t.Fatal("completed sync forgotten on restart")
	}
	if again.Connected() {
		t.Fatal("constructor started a connection")
	}
	_, n := s.Counts()
	if n != 1 {
		t.Fatal("replay duplicated history")
	}
	// Unlinked/offline startup fails promptly and does not retry in a loop.
	check(t, again.Resume(func(LinkEvent) {}))
	if again.Connected() {
		t.Fatal("offline resume connected")
	}
}
