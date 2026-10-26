package wa

import (
	"limmr/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchBackfillIsRestartSafeAndNotRepeated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.db")
	s, e := OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PutChat(model.Chat{JID: "example@s.whatsapp.net", Name: "Example"}); e != nil {
		t.Fatal(e)
	}
	if e = s.PutMessage(model.Message{ID: "one", ChatJID: "example@s.whatsapp.net", Sender: "example@s.whatsapp.net", Content: "synthetic needle", Timestamp: time.Now()}); e != nil {
		t.Fatal(e)
	}
	// Simulate a pre-FTS legacy store and an interrupted first backfill.
	if _, e = s.DB.Exec(`DROP TRIGGER messages_ai; DELETE FROM engine_meta WHERE key='fts_backfilled_v1'; INSERT INTO messages(id,chat_jid,content,timestamp) VALUES('two','example@s.whatsapp.net','legacy needle',CURRENT_TIMESTAMP)`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.DB.QueryRow(`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'needle'`).Scan(&count); e != nil || count != 2 {
		t.Fatal("legacy backfill missing", e, count)
	}
	// An unrelated FTS canary proves a second open does not issue rebuild again.
	if _, e = s.DB.Exec(`INSERT INTO messages_fts(rowid,content) VALUES(99999,'canary')`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.DB.QueryRow(`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'canary'`).Scan(&count); e != nil || count != 1 {
		t.Fatal("repeated full rebuild", e, count)
	}
	if e = s.Rebuild(); e != nil {
		t.Fatal(e)
	}
	if e = s.DB.QueryRow(`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'canary'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("explicit rebuild unavailable", e, count)
	}
}
