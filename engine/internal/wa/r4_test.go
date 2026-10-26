package wa

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type r4OfflineTransport struct{ attempts chan time.Time }

func (f r4OfflineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.attempts <- time.Now()
	return nil, errors.New("synthetic offline dial")
}

// Exercise the pinned whatsmeow retry loop, with every dial intercepted before
// DNS/network. The real two-second backoff is measured rather than mocked.
func TestR4InitialReconnectBackoffAndCancel(t *testing.T) {
	home := t.TempDir()
	l, err := NewLive(home, testStore(t), nil)
	check(t, err)
	defer l.Close()
	jid, err := types.ParseJID(phone)
	check(t, err)
	l.Client.Store.ID = &jid
	fake := r4OfflineTransport{make(chan time.Time, 10)}
	l.Client.SetWebsocketHTTPClient(&http.Client{Transport: fake})
	failed := make(chan struct{}, 10)
	l.Client.AutoReconnectHook = func(error) bool { failed <- struct{}{}; return true }
	check(t, l.Resume(func(LinkEvent) {}))
	read := func() time.Time {
		select {
		case at := <-fake.attempts:
			return at
		case <-time.After(5 * time.Second):
			t.Fatal("initial offline connection did not retry")
			return time.Time{}
		}
	}
	_ = read() // initial Connect, then the first immediate retry
	a, b := read(), read()
	if b.Sub(a) < 1900*time.Millisecond {
		t.Fatal("reconnect spun without backoff")
	}
	// Wait for both failure hooks before cancelling the next (four-second) wait.
	for i := 0; i < 2; i++ {
		select {
		case <-failed:
		case <-time.After(time.Second):
			t.Fatal("missing retry failure callback")
		}
	}
	l.Cancel()
	if l.Connected() || l.Linked() == false {
		t.Fatal("offline cancellation changed the saved-session state")
	}
	select {
	case <-fake.attempts:
		t.Fatal("retry dial after cancellation")
	case <-time.After(4100 * time.Millisecond):
	}
	t.Logf("offline reconnect backoff %.3fms; cancelled pending retry", float64(b.Sub(a))/float64(time.Millisecond))
}

func TestR4TerminalConnectionEvents(t *testing.T) {
	for _, tc := range []struct {
		name             string
		event            any
		linked, outdated bool
	}{
		{"logged_out_stale_device_id", &events.LoggedOut{}, false, false},
		{"outdated", &events.ClientOutdated{}, true, true},
		{"outdated_failure", &events.ConnectFailure{Reason: events.ConnectFailureClientOutdated}, true, true},
		{"stale_keys", &events.CATRefreshError{}, true, false},
		{"replaced", &events.StreamReplaced{}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := offlineLive(t)
			var state LinkEvent
			l.setEmitter(func(e LinkEvent) { state = e })
			l.event(tc.event)
			if state.State != "error" || state.Fix == "" || l.Linked() != tc.linked || l.Connected() || l.Outdated() != tc.outdated {
				t.Fatal("terminal state is missing or dishonest")
			}
			if !tc.linked && l.Reconnect(context.Background()) == nil {
				t.Fatal("reconnect accepted logged-out keys")
			}
		})
	}
}

func TestR4InterruptedRebuildRepairsOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.db")
	s, err := OpenStore(path)
	check(t, err)
	// A crash after schema creation and before the backfill commit must leave no
	// success marker. Model the missing index plus interrupted transaction.
	_, err = s.DB.Exec(`DELETE FROM engine_meta WHERE key='fts_backfilled_v1';
	 INSERT INTO chats(jid) VALUES('example@g.us'); DROP TRIGGER messages_ai;
	 INSERT INTO messages(id,chat_jid,content,timestamp) VALUES('one','example@g.us','repairneedle',CURRENT_TIMESTAMP)`)
	check(t, err)
	tx, err := s.DB.Begin()
	check(t, err)
	_, err = tx.Exec("INSERT INTO messages_fts(messages_fts) VALUES('rebuild')")
	check(t, err)
	check(t, tx.Rollback())
	check(t, s.Close())
	start := time.Now()
	s, err = OpenStore(path)
	check(t, err)
	defer s.Close()
	var count int
	check(t, s.DB.QueryRow(`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'repairneedle'`).Scan(&count))
	if count != 1 {
		t.Fatal("search did not repair itself")
	}
	_, err = s.DB.Exec(`INSERT INTO messages_fts(messages_fts,rank) VALUES('integrity-check',1)`)
	check(t, err)
	t.Logf("reopen and interrupted FTS repair %.3fms", float64(time.Since(start))/float64(time.Millisecond))
}
