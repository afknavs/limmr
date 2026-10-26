package wa

import (
	"context"
	"errors"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"limmr/internal/model"
	"path/filepath"
	"testing"
	"time"
)

const phone = "15550100001@s.whatsapp.net"
const lid = "900000001@lid"

func check(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := OpenStore(filepath.Join(t.TempDir(), "messages.db"))
	check(t, e)
	t.Cleanup(func() { s.Close() })
	return s
}

type mappings struct{ store.NoopStore }

func (*mappings) GetPNForLID(_ context.Context, j types.JID) (types.JID, error) {
	if j.String() == lid {
		return types.ParseJID(phone)
	}
	return types.EmptyJID, nil
}
func (*mappings) GetLIDForPN(_ context.Context, j types.JID) (types.JID, error) {
	if j.String() == phone {
		return types.ParseJID(lid)
	}
	return types.EmptyJID, nil
}
func offlineLive(t *testing.T) *Live {
	t.Helper()
	self, _ := types.ParseJID("15550100099@s.whatsapp.net")
	return &Live{Messages: testStore(t), Client: &whatsmeow.Client{Store: &store.Device{ID: &self, LIDs: &mappings{}, Contacts: &store.NoopStore{}}}}
}
func TestLIDCollisionPreservesMediaRetractionAndFTS(t *testing.T) {
	s := testStore(t)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, j := range []string{phone, lid} {
		check(t, s.PutChat(model.Chat{JID: j}))
		check(t, s.PutMessage(model.Message{ID: "duplicate", ChatJID: j, Sender: lid, Content: "sketches", Timestamp: at}))
	}
	_, e := s.DB.Exec("UPDATE messages SET media_type='image',filename='sketch.jpg',media_key=x'0102',file_length=4,direct_path='/fake',quoted_message_id='quote',deleted_at=? WHERE chat_jid=?", at, lid)
	check(t, e)
	check(t, s.NormalizeMapping(lid, phone))
	check(t, s.NormalizeMapping(lid, phone))
	ms, e := s.Messages(Query{Chat: phone, Search: "sketches"}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].Sender != phone || ms[0].DeletedAt == nil || ms[0].MediaType != "image" || ms[0].QuotedMessageID != "quote" {
		t.Fatal("collision lost metadata")
	}
	var n, size int
	var direct string
	check(t, s.DB.QueryRow("SELECT length(media_key),file_length,direct_path FROM messages WHERE chat_jid=?", phone).Scan(&n, &size, &direct))
	if n != 2 || size != 4 || direct != "/fake" {
		t.Fatal("download metadata lost")
	}
	if e = s.NormalizeMapping(phone, lid); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("invalid mapping accepted")
	}
}
func TestOfflineIngestSeparatesOutgoingSenderAndRecipient(t *testing.T) {
	l := offlineLive(t)
	ctx := context.Background()
	peer, _ := types.ParseJID(lid)
	selfLID, _ := types.ParseJID("900000099@lid")
	pn, _ := types.ParseJID(phone)
	msg := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: selfLID, IsFromMe: true, RecipientAlt: pn}, ID: "outgoing", Timestamp: time.Now()}, Message: &waProto.Message{Conversation: proto.String("Invented outgoing")}}
	check(t, l.ingest(ctx, msg))
	ms, e := l.Messages.Messages(Query{Chat: phone}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].Sender != l.Client.Store.ID.String() || ms[0].ChatJID != phone {
		t.Fatal("outgoing identity mixed with recipient")
	}
}
func TestOfflineStartupNormalizesBareGroupSenders(t *testing.T) {
	l := offlineLive(t)
	group := "120363000001@g.us"
	check(t, l.Messages.PutChat(model.Chat{JID: group}))
	check(t, l.Messages.PutMessage(model.Message{ID: "legacy", ChatJID: group, Sender: "900000001", Content: "Invented", Timestamp: time.Now()}))
	check(t, l.normalizeStoredLIDs(context.Background()))
	ms, e := l.Messages.Messages(Query{Chat: group}, []string{group})
	check(t, e)
	if len(ms) != 1 || ms[0].Sender != phone {
		t.Fatal("uncontacted legacy sender not mapped")
	}
}
func TestIngestCaptionsQuotesReactionsAndRevocations(t *testing.T) {
	l := offlineLive(t)
	pn, _ := types.ParseJID(phone)
	at := time.Now().UTC()
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: pn, Sender: pn}, ID: "image", Timestamp: at}
	image := &waProto.ImageMessage{Caption: proto.String("Searchable caption"), URL: proto.String("https://media.example/fake?x=1"), MediaKey: []byte{1, 2}, FileLength: proto.Uint64(10), ContextInfo: &waProto.ContextInfo{StanzaID: proto.String("quote")}}
	check(t, l.ingest(context.Background(), &events.Message{Info: info, Message: &waProto.Message{ImageMessage: image}}))
	info.ID = "reaction"
	check(t, l.ingest(context.Background(), &events.Message{Info: info, Message: &waProto.Message{ReactionMessage: &waProto.ReactionMessage{Text: proto.String(""), Key: &waProto.MessageKey{ID: proto.String("image")}}}}))
	info.ID = "revoke"
	check(t, l.ingest(context.Background(), &events.Message{Info: info, Message: &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{Type: waProto.ProtocolMessage_REVOKE.Enum(), Key: &waProto.MessageKey{ID: proto.String("image")}}}}))
	ms, e := l.Messages.Messages(Query{ID: "image"}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].Content != "Searchable caption" || ms[0].QuotedMessageID != "quote" || ms[0].DeletedAt == nil {
		t.Fatal("image ingestion metadata")
	}
	ms, e = l.Messages.Messages(Query{ID: "reaction"}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].MediaType != "reaction" || ms[0].Content != "" {
		t.Fatal("reaction removal lost")
	}
}
func TestNewLiveUsesPrivateThrowawaySessionOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LIMMR_HOME", home)
	s := testStore(t)
	l, e := NewLive(home, s, nil)
	check(t, e)
	defer l.Close()
	if l.Linked() || l.Connected() {
		t.Fatal("empty session unexpectedly linked")
	}
}

func TestTimeFiltersOffsetsAndTiedContext(t *testing.T) {
	s := testStore(t)
	check(t, s.PutChat(model.Chat{JID: phone}))
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		ts := at
		if i == 0 {
			ts = at.Add(-time.Hour)
		}
		if i == 4 {
			ts = at.Add(time.Hour)
		}
		check(t, s.PutMessage(model.Message{ID: id, ChatJID: phone, Content: "Invented", Timestamp: ts}))
	}
	// Same instant in an RFC3339 offset must compare against Go/legacy SQL times.
	ms, e := s.Messages(Query{Before: "2026-10-16T17:30:00+05:30"}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].ID != "a" {
		t.Fatal("before filter compares serialized timestamps instead of instants")
	}
	ms, e = s.Messages(Query{After: at.Format(time.RFC3339)}, []string{phone})
	check(t, e)
	if len(ms) != 1 || ms[0].ID != "e" {
		t.Fatal("after filter crossed cutoff")
	}
	ms, e = s.Context(phone, "c", 1, []string{phone})
	check(t, e)
	if len(ms) != 3 || ms[0].ID != "b" || ms[1].ID != "c" || ms[2].ID != "d" {
		t.Fatal("same-second context lost neighbours")
	}
}

type contacts struct {
	store.NoopStore
	entries map[types.JID]types.ContactInfo
}

func (c *contacts) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return c.entries, nil
}
func (c *contacts) GetContact(_ context.Context, j types.JID) (types.ContactInfo, error) {
	return c.entries[j], nil
}

type directory struct {
	people  []model.Person
	groups  []model.Group
	members []model.Membership
}

func (d *directory) PutPerson(p model.Person) error     { d.people = append(d.people, p); return nil }
func (*directory) PutGroup(model.Group) error           { return nil }
func (*directory) PutMembership(model.Membership) error { return nil }
func (d *directory) ReplaceGroups(_ context.Context, gs []model.Group, ms []model.Membership) error {
	d.groups = gs
	d.members = ms
	return nil
}
func TestCopiedDirectoryResolvesNamesAliasesAndCachedMembership(t *testing.T) {
	l := offlineLive(t)
	pn, _ := types.ParseJID(phone)
	lj, _ := types.ParseJID(lid)
	l.Client.Store.ID = &pn
	l.Client.Store.Contacts = &contacts{entries: map[types.JID]types.ContactInfo{pn: {FullName: "Maya Rao"}, lj: {PushName: "Alias push name"}}}
	d := &directory{}
	l.Directory = d
	_, e := l.Messages.DB.Exec("CREATE TABLE wa_groups(jid TEXT,name TEXT,topic TEXT,participant_count INTEGER);CREATE TABLE wa_group_participants(group_jid TEXT,jid TEXT,phone_jid TEXT,lid TEXT,is_admin BOOLEAN,is_super_admin BOOLEAN);INSERT INTO wa_groups VALUES('120363000001@g.us','Design Club','Invented',1);INSERT INTO wa_group_participants VALUES('120363000001@g.us',?,?,?,1,0)", phone, phone, lid)
	check(t, e)
	check(t, l.loadDirectory(context.Background()))
	for _, p := range d.people {
		if p.ID != model.PersonID(phone) || p.Name != "Maya Rao" {
			t.Fatal("LID contact overrode canonical saved name")
		}
	}
	if len(d.groups) != 1 || !d.groups[0].IAmAdmin || len(d.members) != 1 || d.members[0].Person != model.PersonID(phone) || !d.members[0].Admin {
		t.Fatal("cached directory metadata lost")
	}
	aliases, e := l.Messages.Aliases()
	check(t, e)
	if aliases[lid] != phone {
		t.Fatal("cached participant alias not mapped")
	}
}

func TestLiveWatchEventRunsAfterStorageWithMentionsAndSkipsHistory(t *testing.T) {
	l := offlineLive(t)
	peer, _ := types.ParseJID(phone)
	delivered := []StoredEvent{}
	l.OnStored = func(ev StoredEvent) {
		var n int
		check(t, l.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE id=? AND chat_jid=?", ev.Message.ID, ev.Message.ChatJID).Scan(&n))
		if n != 1 {
			t.Fatal("watch notified before storage")
		}
		delivered = append(delivered, ev)
	}
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: peer}, ID: "watch-mention", Timestamp: time.Now().UTC()}
	msg := &events.Message{Info: info, Message: &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("Exam news"), ContextInfo: &waProto.ContextInfo{MentionedJID: []string{l.Client.Store.ID.String()}}}}}
	check(t, l.ingestEvent(context.Background(), msg, true))
	check(t, l.ingestEvent(context.Background(), msg, true))
	msg.Info.ID = "history-only"
	check(t, l.ingest(context.Background(), msg))
	if len(delivered) != 1 || !delivered[0].MentionsMe || delivered[0].Message.Sender != phone {
		t.Fatal("live/history or mention metadata was not preserved")
	}
}

type receiptSink struct {
	calls                int
	chat, sender, status string
}

func (*receiptSink) PutPerson(model.Person) error         { return nil }
func (*receiptSink) PutGroup(model.Group) error           { return nil }
func (*receiptSink) PutMembership(model.Membership) error { return nil }
func (s *receiptSink) RecordReceipt(chat, sender string, ids []string, status string, at time.Time) error {
	s.calls++
	s.chat = chat
	s.sender = sender
	s.status = status
	return nil
}
func TestPassiveReceiptEventAdapter(t *testing.T) {
	l := offlineLive(t)
	sink := &receiptSink{}
	l.Directory = sink
	chat, _ := types.ParseJID("120363000001@g.us")
	sender, _ := types.ParseJID(phone)
	for _, tc := range []struct {
		kind types.ReceiptType
		want string
	}{{types.ReceiptTypeDelivered, "delivered"}, {types.ReceiptTypeRead, "read"}} {
		l.event(&events.Receipt{MessageSource: types.MessageSource{Chat: chat, Sender: sender}, MessageIDs: []string{"invented-id"}, Timestamp: time.Now(), Type: tc.kind})
		if sink.status != tc.want || sink.chat != chat.String() || sink.sender != phone {
			t.Fatal("receipt not forwarded")
		}
	}
	before := sink.calls
	l.event(&events.Receipt{Type: types.ReceiptTypeReadSelf})
	if sink.calls != before {
		t.Fatal("self read treated as delivery")
	}
}
