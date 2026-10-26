package sync_test

import (
	"encoding/json"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"strings"
	"testing"
)

func TestSyncedOwnerProfileUsesReceiverLocalPrivacy(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	jid := "15550000001@s.whatsapp.net"
	id := model.PersonID(jid)
	put(t, a, "people", id, model.Person{ID: id, Name: "Maya Rao", JIDs: []string{jid}, Notes: "OWNER_KNOWLEDGE_CANARY"})
	for _, e := range []*engine{a, b} {
		must(t, e.h.Messages.PutChat(model.Chat{JID: jid, Name: "Maya Rao"}))
		must(t, e.h.Messages.PutMessage(model.Message{ID: "local-message", ChatJID: jid, Sender: jid, Content: "LOCAL_MESSAGE_CANARY", Timestamp: e.now}))
		settings := e.h.Settings()
		settings.HiddenChats = []string{jid}
		must(t, e.h.SetSettings(settings))
	}
	pair(t, a, b)
	exchange(t, a, b)
	owner, err := b.p.Profile(nil, id)
	must(t, err)
	if owner.Person.Notes != "OWNER_KNOWLEDGE_CANARY" {
		t.Fatal("owner knowledge did not sync")
	}
	agent := model.Agent{ID: "receiver-ai", Permissions: model.Defaults(true)}
	if _, err = b.p.Profile(&agent, id); err != model.ErrNotFound {
		t.Fatalf("receiver hidden profile: %v", err)
	}
	xs, err := b.p.PeopleAll(&agent, "Maya", true)
	must(t, err)
	if len(xs) != 0 {
		t.Fatal("receiver search exposed locally hidden synced person")
	}
	out, err := b.p.Export(&agent)
	must(t, err)
	raw, err := json.Marshal(out)
	must(t, err)
	if strings.Contains(string(raw), "OWNER_KNOWLEDGE_CANARY") || strings.Contains(string(raw), id) {
		t.Fatal("receiver export exposed locally hidden synced person")
	}
	// Hidden-chat choices are settings, never replicated owner knowledge. Removing
	// the receiver's local restriction must not be undone by the sender's setting.
	settings := b.h.Settings()
	settings.HiddenChats = nil
	must(t, b.h.SetSettings(settings))
	exchange(t, a, b)
	if len(b.h.Settings().HiddenChats) != 0 || len(a.h.Settings().HiddenChats) != 1 {
		t.Fatal("sync overwrote local hidden-chat settings")
	}
	page, err := a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: 1, Schema: 10})
	must(t, err)
	wire, err := json.Marshal(page)
	must(t, err)
	if strings.Contains(string(wire), "hidden_chats") || strings.Contains(string(wire), "LOCAL_MESSAGE_CANARY") {
		t.Fatal("settings or message content traveled in knowledge sync")
	}
}
