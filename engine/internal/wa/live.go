// Whatsmeow adapter. No presence, typing, mark-read, terminal QR or webhooks.
// Storage, media and LID handling adapted from whatsapp-mcp (see NOTICE).
package wa

import (
	"context"
	"database/sql"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type DirectorySink interface {
	PutPerson(model.Person) error
	PutGroup(model.Group) error
	PutMembership(model.Membership) error
}

// StoredEvent contains live-only metadata captured after successful persistence.
type StoredEvent struct {
	Message    model.Message
	MentionsMe bool
}
type Live struct {
	lastEventAt *time.Time
	// OnStored must be a nonblocking queue handoff. Set before connecting.
	OnStored func(StoredEvent)

	Home      string
	Messages  *Store
	Directory DirectorySink
	Client    *whatsmeow.Client
	container *sqlstore.Container
	emit      func(LinkEvent)
	mu        sync.RWMutex
	syncDone  bool
	outdated  bool
	loggedOut bool
	phoneSeen *time.Time
	lifecycle sync.RWMutex
	closed    bool
	mappingMu sync.Mutex
	mappings  map[string]string
}

func NewLive(home string, m *Store, d DirectorySink) (*Live, error) {
	path := filepath.Join(home, "session.db")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: path}
	c, e := sqlstore.New(context.Background(), "sqlite3", u.String()+"?_foreign_keys=on&_busy_timeout=10000", waLog.Noop)
	if e != nil {
		return nil, e
	}
	dev, e := c.GetFirstDevice(context.Background())
	if e != nil {
		c.Close()
		return nil, e
	}
	l := &Live{Home: home, Messages: m, Directory: d, container: c}
	l.Client = whatsmeow.NewClient(dev, waLog.Noop)
	// A laptop may start offline. Retry the initial dial using whatsmeow's
	// cancellable backoff, just as it retries an established connection loss.
	l.Client.InitialAutoReconnect = true
	l.Client.AddEventHandler(l.event)
	var complete string
	_ = m.DB.QueryRow("SELECT value FROM engine_meta WHERE key='history_complete'").Scan(&complete)
	l.syncDone = complete == "true"
	if dev.ID != nil {
		if e = l.loadDirectory(context.Background()); e != nil {
			l.Close()
			return nil, e
		}
	}
	return l, nil
}
func (l *Live) Close() error {
	l.setEmitter(nil)
	l.Client.Disconnect()
	l.lifecycle.Lock()
	defer l.lifecycle.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.container.Close()
}
func (l *Live) Linked() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return !l.loggedOut && l.Client.Store.ID != nil
}
func (l *Live) Connected() bool {
	return l.Linked() && l.Client.IsConnected() && l.Client.IsLoggedIn()
}
func (l *Live) SyncDone() bool { l.mu.RLock(); defer l.mu.RUnlock(); return l.syncDone }
func (l *Live) Outdated() bool { l.mu.RLock(); defer l.mu.RUnlock(); return l.outdated }
func (l *Live) PhoneSeen() *time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.phoneSeen == nil {
		return nil
	}
	v := *l.phoneSeen
	return &v
}
func (l *Live) setEmitter(emit func(LinkEvent)) { l.mu.Lock(); l.emit = emit; l.mu.Unlock() }
func (l *Live) publish(e LinkEvent) {
	l.mu.RLock()
	emit := l.emit
	l.mu.RUnlock()
	if emit != nil {
		emit(e)
	}
}
func (l *Live) account() *Account {
	phone := ""
	if l.Client.Store.ID != nil {
		p := l.Client.Store.ID.User
		if len(p) > 2 {
			phone = "•••• " + p[len(p)-2:]
		}
	}
	return &Account{Name: l.Client.Store.PushName, PhoneMasked: phone}
}
func (l *Live) Start(ctx context.Context, method, phone string, emit func(LinkEvent)) error {
	l.setEmitter(emit)
	if l.Linked() {
		if e := l.Client.ConnectContext(ctx); e != nil {
			return e
		}
		return nil
	}
	qr, e := l.Client.GetQRChannel(ctx)
	if e != nil {
		return e
	}
	if e = l.Client.ConnectContext(ctx); e != nil {
		return e
	}
	pairedPhone := false
	for {
		select {
		case <-ctx.Done():
			l.Client.Disconnect()
			return ctx.Err()
		case ev, ok := <-qr:
			if !ok {
				return nil
			}
			switch ev.Event {
			case "code":
				if method == "phone" && !pairedPhone {
					code, e := l.Client.PairPhone(ctx, phone, false, whatsmeow.PairClientChrome, "Chrome (macOS)")
					if e != nil {
						return e
					}
					emit(LinkEvent{State: "pair_code", Code: code})
					pairedPhone = true
				} else if method == "qr" {
					emit(LinkEvent{State: "qr", Code: ev.Code, ExpiresIn: int(ev.Timeout.Seconds())})
				}
			case "success":
				return nil
			case "timeout", "error":
				return fmt.Errorf("pairing failed")
			}
		}
	}
}
func (l *Live) Resume(emit func(LinkEvent)) error {
	l.setEmitter(emit)
	if !l.Linked() {
		return nil
	}
	l.publish(LinkEvent{State: "connecting"})
	return l.Client.Connect()
}
func (l *Live) Cancel() { l.setEmitter(nil); l.Client.Disconnect() }
func (l *Live) Unlink(ctx context.Context) error {
	if l.Client.Store.ID != nil {
		return l.Client.Logout(ctx)
	}
	return nil
}
func (l *Live) Reconnect(ctx context.Context) error {
	if !l.Linked() {
		return model.ErrConflict
	}
	l.publish(LinkEvent{State: "connecting"})
	l.Client.Disconnect()
	return l.Client.ConnectContext(ctx)
}
func (l *Live) resolve(ctx context.Context, j, alt types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server == types.DefaultUserServer {
		lid := alt.ToNonAD()
		if lid.Server != types.HiddenUserServer && l.Client.Store.LIDs != nil {
			lid, _ = l.Client.Store.LIDs.GetLIDForPN(ctx, j)
		}
		if lid.Server == types.HiddenUserServer {
			l.normalize(lid, j)
		}
		return j
	}
	if j.Server != types.HiddenUserServer {
		return j
	}
	if alt.Server == types.DefaultUserServer {
		l.normalize(j, alt.ToNonAD())
		return alt.ToNonAD()
	}
	if l.Client.Store.LIDs == nil {
		return j
	}
	if pn, e := l.Client.Store.LIDs.GetPNForLID(ctx, j); e == nil && !pn.IsEmpty() {
		l.normalize(j, pn.ToNonAD())
		return pn.ToNonAD()
	}
	return j
}
func (l *Live) normalize(lid, pn types.JID) {
	l.mappingMu.Lock()
	defer l.mappingMu.Unlock()
	if l.mappings[lid.String()] == pn.String() {
		return
	}
	if l.mappings == nil {
		l.mappings = map[string]string{}
	}
	if l.Messages.NormalizeMapping(lid.String(), pn.String()) != nil {
		return
	}
	if sink, ok := l.Directory.(interface{ NormalizeMapping(string, string) error }); ok {
		if sink.NormalizeMapping(lid.String(), pn.String()) != nil {
			return
		}
	}
	l.mappings[lid.String()] = pn.String()
}
func (l *Live) person(ctx context.Context, j types.JID, push string) string {
	original := j.ToNonAD()
	j = l.resolve(ctx, j, types.EmptyJID)
	name := push
	business := false
	if c, e := l.Client.Store.Contacts.GetContact(ctx, j); e == nil {
		business = c.BusinessName != ""
		if c.FullName != "" {
			name = c.FullName
		} else if c.FirstName != "" {
			name = c.FirstName
		} else if c.PushName != "" {
			name = c.PushName
		} else if push != "" {
			name = push
		} else if c.BusinessName != "" {
			name = c.BusinessName
		}
	}
	if name == "" {
		name = model.FallbackName(j.String())
	}
	if l.Directory != nil {
		_ = l.Directory.PutPerson(model.Person{ID: model.PersonID(j.String()), JIDs: func() []string {
			if original != j {
				return []string{j.String(), original.String()}
			}
			return []string{j.String()}
		}(), Business: business, Broadcast: j.Server == "newsletter" || j.Server == "broadcast", Name: name, PushName: push, UpdatedAt: time.Now().UTC()})
	}
	return name
}
func (l *Live) event(evt any) {
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	if l.closed {
		return
	}
	ctx := context.Background()
	switch v := evt.(type) {
	case *events.Connected:
		l.mu.Lock()
		l.loggedOut, l.outdated = false, false
		l.mu.Unlock()
		if sink, ok := l.Directory.(interface{ KeepOwner(model.Person) error }); ok && l.Client.Store.ID != nil {
			js := []string{l.Client.Store.ID.ToNonAD().String()}
			if !l.Client.Store.LID.IsEmpty() {
				js = append(js, l.Client.Store.LID.ToNonAD().String())
			}
			_ = sink.KeepOwner(model.Person{ID: "me", Name: l.Client.Store.PushName, JIDs: js, UpdatedAt: time.Now().UTC()})
		}
		go func() {
			_ = l.Client.FetchAppState(ctx, appstate.WAPatchCriticalUnblockLow, false, true)
			var done string
			_ = l.Messages.DB.QueryRow("SELECT value FROM engine_meta WHERE key='p1_names_backfilled'").Scan(&done)
			if done != "true" && l.loadDirectory(ctx) == nil {
				_, _ = l.Messages.DB.Exec("INSERT OR REPLACE INTO engine_meta VALUES('p1_names_backfilled','true')")
			}
		}()
		l.publish(LinkEvent{State: "ready", Account: l.account()})
		go func() { _ = l.RefreshGroups(ctx) }()
	case *events.Disconnected:
		l.publish(LinkEvent{State: "connecting"})
	case *events.LoggedOut:
		// Keep status honest even if deleting stale device keys failed upstream.
		l.mu.Lock()
		l.loggedOut = true
		l.mu.Unlock()
		l.publish(LinkEvent{State: "error", Reason: "WhatsApp logged this device out.", Fix: "Link WhatsApp again."})
	case *events.ClientOutdated:
		l.mu.Lock()
		l.outdated = true
		l.mu.Unlock()
		l.publish(LinkEvent{State: "error", Reason: "The WhatsApp client needs an update.", Fix: "Update Limmr."})
	case *events.StreamReplaced:
		l.publish(LinkEvent{State: "error", Reason: "Another client is using this session.", Fix: "Close the other client before reconnecting."})
	case *events.CATRefreshError:
		l.publish(LinkEvent{State: "error", Reason: "WhatsApp session keys could not be refreshed.", Fix: "Link WhatsApp again."})
	case *events.ConnectFailure:
		l.mu.Lock()
		l.outdated = int(v.Reason) == 405
		l.mu.Unlock()
		l.publish(LinkEvent{State: "error", Reason: "WhatsApp rejected the connection.", Fix: "Check Health and update the engine if needed."})
	case *events.Receipt:
		status := ""
		if v.Type == types.ReceiptTypeDelivered {
			status = "delivered"
		}
		if v.Type == types.ReceiptTypeRead {
			status = "read"
		}
		if sink, ok := l.Directory.(interface {
			RecordReceipt(string, string, []string, string, time.Time) error
		}); ok && status != "" {
			_ = sink.RecordReceipt(l.resolve(ctx, v.Chat, types.EmptyJID).String(), l.resolve(ctx, v.Sender, types.EmptyJID).String(), v.MessageIDs, status, v.Timestamp)
		}
	case *events.Message:
		l.mu.Lock()
		now := time.Now().UTC()
		l.lastEventAt = &now
		l.mu.Unlock()
		_ = l.ingestEvent(ctx, v, true)
	case *events.MarkChatAsRead:
		if v.Action.GetRead() {
			_, _ = l.Messages.DB.Exec("UPDATE chats SET unread_count=0 WHERE jid=?", l.resolve(ctx, v.JID, types.EmptyJID).String())
		}
	case *events.Contact:
		l.person(ctx, v.JID, "")
	case *events.PushName:
		j := l.resolve(ctx, v.JID, v.JIDAlt)
		l.person(ctx, j, v.NewPushName)
	case *events.BusinessName:
		l.person(ctx, v.JID, v.NewBusinessName)
		l.business(ctx, v.JID, v.NewBusinessName)
	case *events.HistorySync:
		l.mu.Lock()
		seen := time.Now().UTC()
		l.phoneSeen = &seen
		l.mu.Unlock()
		for _, conv := range v.Data.Conversations {
			chat, e := types.ParseJID(conv.GetID())
			if e != nil {
				continue
			}
			resolved := l.resolve(ctx, chat, types.EmptyJID)
			_ = l.Messages.PutChat(model.Chat{JID: resolved.String(), Name: conv.GetDisplayName()})
			_ = l.Messages.Ephemeral(resolved.String(), conv.GetEphemeralExpiration(), conv.GetEphemeralSettingTimestamp())
			_, _ = l.Messages.DB.Exec("UPDATE chats SET unread_count=? WHERE jid=?", conv.GetUnreadCount(), resolved.String())
			batch := []*events.Message{}
			for _, m := range conv.Messages {
				if m == nil || m.Message == nil {
					continue
				}
				msg, e := l.Client.ParseWebMessage(chat, m.Message)
				if e != nil {
					continue
				}
				alt := msg.Info.SenderAlt
				if msg.Info.IsFromMe {
					alt = msg.Info.RecipientAlt
				}
				msg.Info.Chat = l.resolve(ctx, msg.Info.Chat, alt)
				senderAlt := msg.Info.SenderAlt
				if msg.Info.IsFromMe && l.Client.Store.ID != nil {
					senderAlt = l.Client.Store.ID.ToNonAD()
				}
				msg.Info.Sender = l.resolve(ctx, msg.Info.Sender, senderAlt)
				if msg.Info.Chat.Server != types.GroupServer {
					name := l.person(ctx, msg.Info.Chat, msg.Info.PushName)
					_ = l.Messages.PutChat(model.Chat{JID: msg.Info.Chat.String(), Name: name})
				}
				if msg.Info.Sender.Server != types.GroupServer && !msg.Info.IsFromMe {
					l.person(ctx, msg.Info.Sender, msg.Info.PushName)
				}
				if msg.Info.VerifiedName != nil && !msg.Info.IsFromMe {
					l.business(ctx, msg.Info.Sender, msg.Info.PushName)
				}
				batch = append(batch, msg)
			}
			// Bound transactions so the reader can run between busy group chunks.
			for start := 0; start < len(batch); start += 200 {
				end := min(start+200, len(batch))
				_ = l.Messages.Batch(func(store *Store) error {
					for _, msg := range batch[start:end] {
						if e := l.ingestInto(ctx, msg, false, store); e != nil {
							return e
						}
					}
					return nil
				})
			}

		}
		c, m := l.Messages.Counts()
		progress := int(v.Data.GetProgress())
		l.publish(LinkEvent{State: "syncing", Progress: progress, Chats: c, Messages: m})
		if progress >= 100 {
			l.mu.Lock()
			l.syncDone = true
			l.mu.Unlock()
			_, _ = l.Messages.DB.Exec("INSERT INTO engine_meta VALUES('history_complete','true') ON CONFLICT(key) DO UPDATE SET value=excluded.value")
			l.publish(LinkEvent{State: "ready", Account: l.account()})
		}
	case *events.GroupInfo:
		if v.Ephemeral != nil {
			expiration := uint32(0)
			if v.Ephemeral.IsEphemeral {
				expiration = v.Ephemeral.DisappearingTimer
			}
			_ = l.Messages.Ephemeral(v.JID.String(), expiration, v.Timestamp.Unix())
		}
		go func() { _ = l.RefreshGroups(ctx) }()
	}
}
func (l *Live) ingest(ctx context.Context, v *events.Message) error {
	return l.ingestEvent(ctx, v, false)
}
func (l *Live) ingestEvent(ctx context.Context, v *events.Message, incoming bool) error {
	return l.ingestInto(ctx, v, incoming, l.Messages)
}
func (l *Live) ingestInto(ctx context.Context, v *events.Message, incoming bool, store *Store) error {
	if v.Message == nil {
		return nil
	}
	chat, sender := v.Info.Chat, v.Info.Sender
	name := ""
	if store.tx == nil {
		alt := v.Info.SenderAlt
		if v.Info.IsFromMe {
			alt = v.Info.RecipientAlt
		}
		chat = l.resolve(ctx, v.Info.Chat, alt)
		senderAlt := v.Info.SenderAlt
		if v.Info.IsFromMe && l.Client.Store.ID != nil {
			senderAlt = l.Client.Store.ID.ToNonAD()
		}
		sender = l.resolve(ctx, v.Info.Sender, senderAlt)
		name = ""
		if chat.Server != types.GroupServer {
			name = l.person(ctx, chat, v.Info.PushName)
		}
		if sender.Server != types.GroupServer && !v.Info.IsFromMe {
			l.person(ctx, sender, v.Info.PushName)
		}

	}
	if store.tx == nil && v.Info.VerifiedName != nil && !v.Info.IsFromMe {
		l.business(ctx, sender, v.Info.PushName)
	}
	at := v.Info.Timestamp
	if e := store.PutChat(model.Chat{JID: chat.String(), Name: name, LastMessageTime: &at}); e != nil {
		return e
	}
	msg := v.Message
	if p := msg.GetProtocolMessage(); p != nil {
		switch p.GetType() {
		case waProto.ProtocolMessage_REVOKE:
			_, e := store.exec("UPDATE messages SET deleted_at=? WHERE id=? AND chat_jid=? AND deleted_at IS NULL", at, p.GetKey().GetID(), chat.String())
			return e
		case waProto.ProtocolMessage_EPHEMERAL_SETTING:
			ts := p.GetEphemeralSettingTimestamp()
			if ts == 0 {
				ts = at.Unix()
			}
			return store.Ephemeral(chat.String(), p.GetEphemeralExpiration(), ts)
		}
	}
	content := extractTextContent(msg)
	mt, filename, url, key, sha, enc, size := extractMediaInfo(msg, at, v.Info.ID)
	quote, _, _ := extractQuotedMessageInfo(msg)
	if ci := contextInfo(msg); ci != nil {
		_ = store.Ephemeral(chat.String(), ci.GetExpiration(), ci.GetEphemeralSettingTimestamp())
	}
	if r := msg.GetReactionMessage(); r != nil {
		mt = "reaction"
		content = r.GetText()
		filename = r.GetKey().GetID()
	}
	m := model.Message{ID: v.Info.ID, ChatJID: chat.String(), Sender: sender.String(), Content: content, Timestamp: at, IsFromMe: v.Info.IsFromMe, MediaType: mt, Filename: filename, QuotedMessageID: quote}
	var existed int
	if incoming && l.OnStored != nil {
		if err := l.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=? AND id=?", m.ChatJID, m.ID).Scan(&existed); err != nil {
			return err
		}
	}
	if e := store.PutMessage(m); e != nil {
		return e
	}
	if mt != "" && mt != "reaction" {
		direct := ""
		if u, e := urlParse(url); e == nil {
			direct = u
		}
		_, e := store.exec("UPDATE messages SET url=?,media_key=?,file_sha256=?,file_enc_sha256=?,file_length=?,direct_path=? WHERE id=? AND chat_jid=?", url, key, sha, enc, size, direct, m.ID, m.ChatJID)
		if e != nil {
			return e
		}
	}
	if incoming && existed == 0 && !v.Info.IsFromMe {
		_, _ = store.exec("UPDATE chats SET unread_count=unread_count+1 WHERE jid=?", m.ChatJID)
	}
	if incoming && existed == 0 && l.OnStored != nil {
		mentions := false
		if ci := contextInfo(msg); ci != nil {
			for _, raw := range ci.GetMentionedJID() {
				jid, err := types.ParseJID(raw)
				if err == nil && l.Client.Store.ID != nil && l.resolve(ctx, jid, types.EmptyJID) == l.Client.Store.ID.ToNonAD() {
					mentions = true
					break
				}
			}
		}
		l.OnStored(StoredEvent{Message: m, MentionsMe: mentions})
	}
	return nil
}
func urlParse(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	return u.RequestURI(), nil
}
func contextInfo(msg *waProto.Message) *waProto.ContextInfo {
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	}
	return nil
}
func (l *Live) RefreshGroups(ctx context.Context) error {
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	if l.closed {
		return model.ErrConflict
	}
	groups, e := l.Client.GetJoinedGroups(ctx)
	if e != nil {
		return fmt.Errorf("group refresh unavailable")
	}
	if l.Directory == nil {
		return nil
	}
	return l.refreshDirectory(ctx, groups)
}
func (l *Live) refreshDirectory(ctx context.Context, groups []*types.GroupInfo) error {
	// Snapshot membership changes replace stale memberships via the sink when available.
	if reset, ok := l.Directory.(interface {
		ReplaceGroups(context.Context, []model.Group, []model.Membership) error
	}); ok {
		gs := []model.Group{}
		ms := []model.Membership{}
		for _, g := range groups {
			admin := false
			for _, p := range g.Participants {
				if p.LID.Server == types.HiddenUserServer && p.PhoneNumber.Server == types.DefaultUserServer {
					l.normalize(p.LID.ToNonAD(), p.PhoneNumber.ToNonAD())
				}
				j := l.resolve(ctx, p.JID, p.PhoneNumber)
				l.person(ctx, j, "")
				isAdmin := p.IsAdmin || p.IsSuperAdmin
				if l.Client.Store.ID != nil && j == l.Client.Store.ID.ToNonAD() {
					admin = isAdmin
				}
				ms = append(ms, model.Membership{Group: g.JID.String(), Person: model.PersonID(j.String()), Admin: isAdmin})
			}
			parent := ""
			if !g.LinkedParentJID.IsEmpty() {
				parent = g.LinkedParentJID.String()
			}
			chatSettings, e := l.Client.Store.ChatSettings.GetChatSettings(ctx, g.JID)
			if e != nil {
				return e
			}
			gs = append(gs, model.Group{Muted: !chatSettings.MutedUntil.IsZero() && (chatSettings.MutedUntil.After(time.Now()) || chatSettings.MutedUntil.Unix() < 0), Archived: chatSettings.Archived, IsCommunity: g.IsParent, ParentJID: parent, JID: g.JID.String(), Name: g.Name, Description: g.Topic, MemberCount: len(g.Participants), IAmAdmin: admin})
			if e := l.Messages.PutChat(model.Chat{JID: g.JID.String(), Name: g.Name}); e != nil {
				return e
			}
		}
		return reset.ReplaceGroups(ctx, gs, ms)
	}
	return fmt.Errorf("directory snapshot not supported")
}
func (l *Live) RequestHistory(ctx context.Context) error {
	rows, e := l.Messages.DB.Query("SELECT chat_jid,id,timestamp,is_from_me FROM messages m WHERE timestamp=(SELECT MIN(timestamp) FROM messages WHERE chat_jid=m.chat_jid) ORDER BY chat_jid")
	if e != nil {
		return e
	}
	infos := []types.MessageInfo{}
	for rows.Next() {
		var chat string
		var info types.MessageInfo
		if e = rows.Scan(&chat, &info.ID, &info.Timestamp, &info.IsFromMe); e != nil {
			rows.Close()
			return e
		}
		info.Chat, e = types.ParseJID(chat)
		if e != nil {
			rows.Close()
			return e
		}
		infos = append(infos, info)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(infos) == 0 {
		return model.ErrConflict
	}
	for _, info := range infos {
		if _, e = l.Client.SendPeerMessage(ctx, l.Client.BuildHistorySyncRequest(&info, 50)); e != nil {
			return fmt.Errorf("history request unavailable")
		}
	}
	return nil
}
func (l *Live) Send(ctx context.Context, a model.Approval) (string, error) {
	if !l.Connected() {
		return "", fmt.Errorf("disconnected")
	}
	chat, e := types.ParseJID(a.ChatJID)
	if e != nil {
		return "", model.ErrInvalid
	}
	recipient := chat
	if chat.Server == types.DefaultUserServer {
		if lid, err := l.Client.Store.LIDs.GetLIDForPN(ctx, chat); err == nil && !lid.IsEmpty() {
			recipient = lid
		} else if users, err := l.Client.GetUserInfo(ctx, []types.JID{chat}); err == nil && !users[chat].LID.IsEmpty() {
			recipient = users[chat].LID
		}
	}
	msg := &waProto.Message{Conversation: proto.String(a.Text)}
	if a.ReplyTo != "" {
		ms, e := l.Messages.Messages(Query{Chat: a.ChatJID, ID: a.ReplyTo, Limit: 1}, []string{a.ChatJID})
		if e != nil || len(ms) != 1 {
			return "", model.ErrNotFound
		}
		original := ms[0]
		sender, e := types.ParseJID(original.Sender)
		if e != nil {
			return "", model.ErrInvalid
		}
		if a.Kind == "reaction" {
			msg = l.Client.BuildReaction(recipient, sender, a.ReplyTo, a.Text)
		} else {
			msg = &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String(a.Text), ContextInfo: &waProto.ContextInfo{StanzaID: proto.String(a.ReplyTo), Participant: proto.String(sender.String()), QuotedMessage: &waProto.Message{Conversation: proto.String(original.Content)}}}}
		}
	}
	if a.Kind == "file" {
		if a.Attachment == nil {
			return "", model.ErrInvalid
		}
		data, e := os.ReadFile(filepath.Join(l.Home, "outbox", filepath.Base(a.Attachment.ID)))
		if e != nil || dbutil.Digest(string(data)) != a.Attachment.SHA256 {
			return "", model.ErrInvalid
		}
		msg, e = uploadMediaMessage(ctx, l.Client, data, a, contextInfo(msg))
		if e != nil {
			return "", e
		}
	}
	var expiration uint32
	var setAt int64
	e = l.Messages.DB.QueryRow("SELECT ephemeral_expiration,ephemeral_setting_timestamp FROM chats WHERE jid=?", a.ChatJID).Scan(&expiration, &setAt)
	if e != nil && e != sql.ErrNoRows {
		return "", e
	}
	if expiration > 0 && setAt > 0 && a.Kind != "reaction" {
		if msg.Conversation != nil {
			msg = &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: msg.Conversation}}
		}
		ci := contextInfo(msg)
		if ci == nil {
			ci = &waProto.ContextInfo{}
		}
		ci.Expiration = proto.Uint32(expiration)
		ci.EphemeralSettingTimestamp = proto.Int64(setAt)
		ci.DisappearingMode = &waProto.DisappearingMode{Initiator: waProto.DisappearingMode_CHANGED_IN_CHAT.Enum(), Trigger: waProto.DisappearingMode_CHAT_SETTING.Enum()}
		if msg.ExtendedTextMessage != nil {
			msg.ExtendedTextMessage.ContextInfo = ci
		}
		if msg.DocumentMessage != nil {
			msg.DocumentMessage.ContextInfo = ci
		}
		if msg.ImageMessage != nil {
			msg.ImageMessage.ContextInfo = ci
		}
		if msg.VideoMessage != nil {
			msg.VideoMessage.ContextInfo = ci
		}
		if msg.AudioMessage != nil {
			msg.AudioMessage.ContextInfo = ci
		}
	}
	resp, e := l.Client.SendMessage(ctx, recipient, msg, whatsmeow.SendRequestExtra{ID: a.MessageID})
	if e != nil {
		return "", fmt.Errorf("send failed")
	}
	sender := "me"
	if l.Client.Store.ID != nil {
		sender = l.Client.Store.ID.ToNonAD().String()
	}
	m := model.Message{ID: resp.ID, ChatJID: a.ChatJID, Sender: sender, Content: a.Text, Timestamp: resp.Timestamp, IsFromMe: true, QuotedMessageID: a.ReplyTo}
	if a.Kind == "file" {
		m.MediaType = "document"
		if msg.ImageMessage != nil {
			m.MediaType = "image"
		}
		if msg.VideoMessage != nil {
			m.MediaType = "video"
		}
		if msg.AudioMessage != nil {
			m.MediaType = "audio"
		}
		m.Filename = a.Attachment.Name
	}
	if a.Kind == "reaction" {
		m.MediaType = "reaction"
		m.Filename = a.ReplyTo
	}
	if e = l.Messages.PutMessage(m); e != nil {
		return resp.ID, nil
	}
	return resp.ID, nil
}
func (l *Live) Download(ctx context.Context, chat, id string) (string, error) {
	var mt, url, direct string
	var key, sha, enc []byte
	var size uint64
	e := l.Messages.DB.QueryRow("SELECT COALESCE(media_type,''),COALESCE(url,''),COALESCE(direct_path,''),media_key,file_sha256,file_enc_sha256,COALESCE(file_length,0) FROM messages WHERE id=? AND chat_jid=?", id, chat).Scan(&mt, &url, &direct, &key, &sha, &enc, &size)
	if e != nil {
		return "", model.ErrNotFound
	}
	if size > 100<<20 || size == 0 || len(key) == 0 {
		return "", model.ErrInvalid
	}
	t := whatsmeow.MediaDocument
	switch mt {
	case "image", "sticker":
		t = whatsmeow.MediaImage
	case "audio":
		t = whatsmeow.MediaAudio
	case "video":
		t = whatsmeow.MediaVideo
	case "document":
	default:
		return "", model.ErrInvalid
	}
	if direct == "" {
		direct, _ = urlParse(url)
	}
	data, e := l.Client.Download(ctx, &MediaDownloader{url, direct, key, size, sha, enc, t})
	if e != nil {
		return "", fmt.Errorf("download unavailable")
	}
	dir := filepath.Join(l.Home, "media")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, dbutil.Digest(chat+"\x00"+id))
	return path, os.WriteFile(path, data, 0600)
}

// Directory metadata only; never sends, acknowledges or marks a chat read.
func (l *Live) business(ctx context.Context, j types.JID, name string) {
	if l.Directory == nil {
		return
	}
	j = l.resolve(ctx, j, types.EmptyJID)
	if sink, ok := l.Directory.(interface{ MarkBusiness(string) error }); ok {
		_ = sink.MarkBusiness(j.String())
		return
	}
	name = l.person(ctx, j, name)
	_ = l.Directory.PutPerson(model.Person{ID: model.PersonID(j.String()), JIDs: []string{j.String()}, Name: name, Business: true, UpdatedAt: time.Now().UTC()})
}

func (l *Live) LastEventAt() *time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastEventAt == nil {
		return nil
	}
	v := *l.lastEventAt
	return &v
}

// NewMessageID reserves provenance before a network send can be observed.
func NewMessageID() string { return whatsmeow.GenerateMessageID() }
