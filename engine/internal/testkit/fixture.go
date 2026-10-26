// Package testkit supplies isolated invented fixtures for engine tests.
package testkit

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/wa"
	"path/filepath"
	"testing"
	"time"
)

const Phone = "15550100001@s.whatsapp.net"
const Group = "120363000001@g.us"

var Epoch = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func Must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func Open(t testing.TB) (*hub.Service, *people.Service, *wa.Link) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("LIMMR_HOME", home)
	repo, err := hub.Open(filepath.Join(home, "hub.db"))
	Must(t, err)
	t.Cleanup(func() { repo.Close() })
	messages, err := wa.OpenStore(filepath.Join(home, "messages.db"))
	Must(t, err)
	t.Cleanup(func() { messages.Close() })
	h := hub.New(repo, messages, home)
	h.Now = func() time.Time { return Epoch }
	h.Messenger = &wa.Fake{Messages: messages}
	return h, &people.Service{Hub: h}, wa.NewLink(&wa.Fake{})
}
func Seed(t testing.TB, h *hub.Service, p *people.Service) {
	t.Helper()
	Must(t, h.Messages.PutChat(model.Chat{JID: Phone, Name: "Maya Rao"}))
	Must(t, h.Messages.PutChat(model.Chat{JID: Group, Name: "Design Club"}))
	Must(t, h.Messages.PutMessage(model.Message{ID: "owner-sent", Content: "A reply", ChatJID: Phone, IsFromMe: true, Timestamp: Epoch.AddDate(-2, 0, 0)}))
	Must(t, p.PutPerson(model.Person{ID: model.PersonID(Phone), JIDs: []string{Phone}, Name: "Maya Rao", UpdatedAt: Epoch}))
	Must(t, p.PutGroup(model.Group{JID: Group, Name: "Design Club", MemberCount: 1}))
	Must(t, p.PutMembership(model.Membership{Group: Group, Person: model.PersonID(Phone)}))
}
