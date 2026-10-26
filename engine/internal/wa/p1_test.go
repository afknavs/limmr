package wa

import (
	"context"
	"errors"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"limmr/internal/model"
	"testing"
	"time"
)

type nameContacts struct {
	store.NoopStore
	contact types.ContactInfo
}

func (n *nameContacts) GetContact(context.Context, types.JID) (types.ContactInfo, error) {
	return n.contact, nil
}
func TestP1ContactNamePriorityAndLIDFallback(t *testing.T) {
	l := offlineLive(t)
	j, _ := types.ParseJID(phone)
	for _, tc := range []struct {
		c          types.ContactInfo
		push, want string
	}{{types.ContactInfo{FullName: "Maya Contact", FirstName: "Maya", PushName: "Push", BusinessName: "Business"}, "Event push", "Maya Contact"}, {types.ContactInfo{FirstName: "Maya", PushName: "Push"}, "", "Maya"}, {types.ContactInfo{PushName: "Push", BusinessName: "Business"}, "", "Push"}, {types.ContactInfo{BusinessName: "Business"}, "", "Business"}, {types.ContactInfo{}, "", "+15550100001"}} {
		l.Client.Store.Contacts = &nameContacts{contact: tc.c}
		if got := l.person(context.Background(), j, tc.push); got != tc.want {
			t.Fatalf("name preference: %s", got)
		}
	}
	l.Client.Store.Contacts = &store.NoopStore{}
	unknown, _ := types.ParseJID("999999999@lid")
	if got := l.person(context.Background(), unknown, ""); got != "Unknown" {
		t.Fatal("raw LID leaked")
	}
}
func TestP1BatchAtomicityAndMetadata(t *testing.T) {
	l := offlineLive(t)
	ctx := context.Background()
	pn, _ := types.ParseJID(phone)
	at := time.Now().UTC()
	msg := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: pn, Sender: pn}, ID: "history-image", Timestamp: at}, Message: &waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("Synthetic image"), URL: proto.String("https://example.invalid/image"), MediaKey: []byte{1}, FileLength: proto.Uint64(5), ContextInfo: &waProto.ContextInfo{StanzaID: proto.String("quote")}}}}
	// No resolve/directory locks or message reads may run while holding this transaction.
	check(t, l.Messages.Batch(func(b *Store) error { return l.ingestInto(ctx, msg, false, b) }))
	ms, e := l.Messages.Messages(Query{Chat: phone}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].QuotedMessageID != "quote" || ms[0].MediaType != "image" {
		t.Fatal("batch lost metadata")
	}
	sentinel := errors.New("rollback")
	e = l.Messages.Batch(func(b *Store) error {
		if e := b.PutMessage(model.Message{ID: "rolled-back", ChatJID: phone, Content: "Synthetic", Timestamp: at}); e != nil {
			return e
		}
		return sentinel
	})
	if e != sentinel {
		t.Fatal("missing rollback error")
	}
	ms, e = l.Messages.Messages(Query{Chat: phone, ID: "rolled-back"}, []string{phone})
	check(t, e)
	if len(ms) != 0 {
		t.Fatal("partial transaction persisted")
	}
}
func BenchmarkP1HistoryWrites(b *testing.B) {
	for _, batch := range []bool{false, true} {
		name := "individual"
		if batch {
			name = "batch200"
		}
		b.Run(name, func(b *testing.B) {
			s, e := OpenStore(b.TempDir() + "/messages.db")
			if e != nil {
				b.Fatal(e)
			}
			b.Cleanup(func() { s.Close() })
			checkB := func(e error) {
				if e != nil {
					b.Fatal(e)
				}
			}
			checkB(s.PutChat(model.Chat{JID: phone}))
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if batch {
					checkB(s.Batch(func(tx *Store) error {
						for i := 0; i < 200; i++ {
							if e := tx.PutMessage(model.Message{ID: string(rune(i + 1)), ChatJID: phone, Content: "Synthetic planning", Timestamp: time.Now()}); e != nil {
								return e
							}
						}
						return nil
					}))
				} else {
					for i := 0; i < 200; i++ {
						checkB(s.PutMessage(model.Message{ID: string(rune(i + 1)), ChatJID: phone, Content: "Synthetic planning", Timestamp: time.Now()}))
					}
				}
			}
		})
	}
}

func TestF2BusinessMetadataPreservesContactName(t *testing.T) {
	l := offlineLive(t)
	sink := &directory{}
	l.Directory = sink
	j, _ := types.ParseJID(phone)
	l.Client.Store.Contacts = &nameContacts{contact: types.ContactInfo{FullName: "Northfield Bank adviser", BusinessName: "Northfield Bank"}}
	l.person(context.Background(), j, "Alerts")
	if len(sink.people) == 0 || !sink.people[len(sink.people)-1].Business || sink.people[len(sink.people)-1].Name != "Northfield Bank adviser" {
		t.Fatal("business contact metadata/name lost")
	}
	l.business(context.Background(), j, "Northfield Bank")
	last := sink.people[len(sink.people)-1]
	if !last.Business || last.Name != "Northfield Bank adviser" {
		t.Fatal("verification replaced contact name")
	}
}
