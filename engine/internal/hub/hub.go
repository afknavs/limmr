package hub

import (
	"context"
	"crypto/subtle"
	"limmr/internal/attachments"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Messenger is invoked only by validated draft delivery or legacy owner decisions.
type Messenger interface {
	Send(context.Context, model.Approval) (string, error)
}
type Service struct {
	MayRunAutomations  func(context.Context) bool
	TaskSendValidate   func(model.Agent, string, string) error
	TaskSent           func(model.Draft) error
	TaskSuggestionDone func(model.Suggestion) error
	updates            updateGate
	parent             *Service
	Repo               *Repo
	Messages           *wa.Store
	Messenger          Messenger
	Home               string
	Now                func() time.Time
	mu                 sync.Mutex
	activityMu         sync.Mutex
	Watches            *triggers.Engine
	WatchStore         *triggers.SQLStore
	watchers           map[chan model.Activity]bool
}

func New(r *Repo, m *wa.Store, home string) *Service {
	s := &Service{Repo: r, Messages: m, Home: home, Now: time.Now, watchers: map[chan model.Activity]bool{}}
	var settings model.Settings
	if r.Get("settings", "main", &settings) == model.ErrNotFound {
		_ = r.Put("settings", "main", model.Settings{HiddenChats: []string{}, RetentionDays: 180})
	}
	// Optional presentation counters are retried (and errors surfaced) by the stats route.
	_ = s.initSyncStats()
	s.PruneOutbox()
	return s
}
func (s *Service) Settings() model.Settings {
	var v model.Settings
	_ = s.Repo.Get("settings", "main", &v)
	if v.CurrentDays == 0 {
		v.CurrentDays = 180
	}
	if v.MaxCircles == 0 {
		v.MaxCircles = 8
	}
	return v
}
func (s *Service) SetSettings(v model.Settings) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if v.AccessoryCue.Color != "" && !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(v.AccessoryCue.Color) {
		return model.ErrInvalid
	}
	if q := v.AccessoryCue.QuietHours; q != nil {
		for _, clock := range []string{q.Start, q.End} {
			if _, err := time.Parse("15:04", clock); err != nil || len(clock) != 5 {
				return model.ErrInvalid
			}
		}
		if q.Timezone != "" {
			if _, err := time.LoadLocation(q.Timezone); err != nil {
				return model.ErrInvalid
			}
		}
	}
	if v.CurrentDays == 0 {
		v.CurrentDays = 180
	}
	if v.MaxCircles < 0 || v.MaxCircles > 100 {
		return model.ErrInvalid
	}
	if v.CurrentDays < 1 || v.CurrentDays > 3650 {
		return model.ErrInvalid
	}
	if v.RetentionDays < 1 || v.RetentionDays > 3650 || len(v.Owner) > 100 {
		return model.ErrInvalid
	}
	var owner model.OwnerProfile
	if s.Repo.Get("settings", "owner_profile", &owner) == nil && owner.Name != "" {
		v.Owner = owner.Name
	}
	return s.Repo.Put("settings", "main", v)
}
func (s *Service) Agent(id string) (model.Agent, error) {
	var a model.Agent
	e := s.Repo.Get("agents", id, &a)
	if e == nil {
		a = s.PresentAgent(a)
	}
	return a, e
}
func (s *Service) Agents() ([]model.Agent, error) {
	xs, e := List[model.Agent](s.Repo, "agents")
	for i := range xs {
		xs[i] = s.PresentAgent(xs[i])
	}
	return xs, e
}
func (s *Service) CreateAgent(name, kind, transport string) (model.Agent, string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 100 || !model.Contains([]string{"claude_code", "codex", "chatgpt", "claude_ai", "cursor", "limerence", "custom"}, kind) || !model.Contains([]string{"local", "remote"}, transport) {
		return model.Agent{}, "", model.ErrInvalid
	}
	a := model.Agent{ID: dbutil.ID(), Name: name, Kind: kind, Transport: transport, CreatedAt: s.Now().UTC(), Enabled: true, Preset: "own", Permissions: model.Preset("own")}
	e := s.Repo.Put("agents", a.ID, a)
	if e != nil {
		return a, "", e
	}
	if transport == "remote" {
		return s.PresentAgent(a), "", nil
	}
	t, e := s.Rotate(a.ID)
	return s.PresentAgent(a), t, e
}
func (s *Service) UpdateAgent(a model.Agent) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if !a.Permissions.Valid() || strings.TrimSpace(a.Name) == "" || utf8.RuneCountInString(a.Name) > 100 {
		return model.ErrInvalid
	}
	result, e := s.Repo.DB.Exec("UPDATE objects SET data=json_set(data,'$.name',?,'$.enabled',json(?),'$.permissions',json(?),'$.preset',?) WHERE bucket='agents' AND id=?", a.Name, dbutil.JSON(a.Enabled), dbutil.JSON(a.Permissions), a.Preset, a.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return model.ErrNotFound
	}
	return nil
}
func (s *Service) Rotate(id string) (string, error) {
	a, e := s.Agent(id)
	if e != nil {
		return "", e
	}
	if a.Transport != "local" || a.ID == "this-mac" {
		return "", model.ErrInvalid
	}
	token := dbutil.ID() + dbutil.ID()
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM credentials WHERE agent_id=?", id); e != nil {
		return "", e
	}
	if _, e = tx.Exec("INSERT INTO credentials VALUES(?,?)", dbutil.Digest(token), id); e != nil {
		return "", e
	}
	return token, tx.Commit()
}
func (s *Service) Revoke(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM trigger_run_credentials WHERE agent_id=?", id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM credentials WHERE agent_id=?", id); e != nil {
		return e
	}
	// Removing remote client bindings also invalidates in-memory grants: their
	// client can no longer authenticate after a profile is re-enabled.
	if _, e = tx.Exec("DELETE FROM objects WHERE bucket IN ('oauth_tokens','oauth_clients') AND json_extract(data,'$.agent_id')=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) DeleteAgent(id string) error {
	if e := s.Revoke(id); e != nil {
		return e
	}
	return s.Repo.Delete("agents", id)
}
func (s *Service) Authenticate(token string) (model.Agent, error) {
	if len(token) < 32 {
		return model.Agent{}, model.ErrForbidden
	}
	var id string
	e := s.Repo.DB.QueryRow("SELECT agent_id FROM credentials WHERE digest=?", dbutil.Digest(token)).Scan(&id)
	if e != nil {
		return model.Agent{}, model.ErrForbidden
	}
	return s.ActiveAgent(id)
}
func (s *Service) ActiveAgent(id string) (model.Agent, error) {
	result, e := s.Repo.DB.Exec("UPDATE objects SET data=json_set(data,'$.last_seen_at',?) WHERE bucket='agents' AND id=? AND json_extract(data,'$.enabled')=1", s.Now().UTC().Format(time.RFC3339Nano), id)
	if e != nil {
		return model.Agent{}, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return model.Agent{}, e
	}
	if n != 1 {
		return model.Agent{}, model.ErrForbidden
	}
	a, e := s.Agent(id)
	if e != nil || !a.Enabled || a.ID == "this-mac" || a.Kind == "on_device" {
		return model.Agent{}, model.ErrForbidden
	}
	return a, nil
}
func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *Service) Visible(a *model.Agent) ([]model.Chat, []string, error) {
	cs, e := s.Messages.Chats()
	if e != nil {
		return nil, nil, e
	}
	out := []model.Chat{}
	ids := []string{}
	allowed := s.ChatAccess(a)
	for _, c := range cs {
		if allowed(c.JID) {
			out = append(out, c)
			ids = append(ids, c.JID)
		}
	}
	return out, ids, nil
}

// ChatAccess takes a snapshot before opening any streaming DB query.
func (s *Service) ChatAccess(a *model.Agent) func(string) bool {
	if a == nil {
		return func(string) bool { return true }
	}
	aliases, e := s.Messages.Aliases()
	if e != nil {
		return func(string) bool { return false }
	}
	canonical := func(j string) string {
		if v, ok := aliases[j]; ok {
			return v
		}
		return j
	}
	var settings model.Settings
	if s.Repo.Get("settings", "main", &settings) != nil {
		return func(string) bool { return false }
	}
	hidden := []string{}
	for _, j := range settings.HiddenChats {
		hidden = append(hidden, canonical(j))
	}
	p := a.Permissions
	p.Chats.JIDs = append([]string{}, p.Chats.JIDs...)
	for i, j := range p.Chats.JIDs {
		p.Chats.JIDs[i] = canonical(j)
	}
	// Snapshot confirmed memberships: unconfirmed proposals never sort a chat.
	cs, e := List[model.Circle](s.Repo, "circles")
	if e != nil {
		return func(string) bool { return false }
	}
	sorted, granted := map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		if c.Confirmed {
			for _, chat := range c.Chats {
				j := canonical(chat.ChatJID)
				sorted[j] = true
				if p.Access.AllowsCircle(c.ID) {
					granted[j] = true
				}
			}
		}
	}
	ended := p.Access.Ended(s.Now())
	return func(j string) bool {
		j = canonical(j)
		return !ended && p.Allows(j, hidden) && (p.Access.Circles == nil || granted[j] || p.Access.Unsorted && !sorted[j])
	}
}
func (s *Service) HiddenChatAccess() func(string) bool {
	aliases, e := s.Messages.Aliases()
	if e != nil {
		return func(string) bool { return true }
	}
	canonical := func(j string) string {
		if v, ok := aliases[j]; ok {
			return v
		}
		return j
	}
	var settings model.Settings
	if s.Repo.Get("settings", "main", &settings) != nil {
		return func(string) bool { return true }
	}
	hidden := map[string]bool{}
	for _, j := range settings.HiddenChats {
		hidden[canonical(j)] = true
	}
	return func(j string) bool { return hidden[canonical(j)] }
}
func (s *Service) HiddenChat(jid string) bool              { return s.HiddenChatAccess()(jid) }
func (s *Service) CanChat(a *model.Agent, jid string) bool { return s.ChatAccess(a)(jid) }
func (s *Service) RequestSend(a model.Agent, kind, chat, text, _, reply, attachment string) (model.Approval, error) {
	done, err := s.beginOutbound()
	if err != nil {
		return model.Approval{}, err
	}
	defer done()
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var e error
	a, e = s.Agent(a.ID)
	if e != nil || !a.Enabled || !a.Permissions.RequestSend {
		return model.Approval{}, model.ErrForbidden
	}
	if !s.CanChat(&a, chat) {
		return model.Approval{}, model.ErrNotFound
	}
	if _, _, e := s.sendAgent(a.ID, chat); e != nil {
		return model.Approval{}, e
	}
	if !model.Contains([]string{"message", "file", "reaction"}, kind) || len(text) > 65536 || (kind == "message" && text == "") || (kind == "reaction" && reply == "") {
		return model.Approval{}, model.ErrInvalid
	}
	if reply != "" {
		ms, e := s.Messages.Messages(wa.Query{Chat: chat, ID: reply, Limit: 1}, []string{chat})
		if e != nil || len(ms) != 1 {
			return model.Approval{}, model.ErrNotFound
		}
	}
	ap := model.Approval{Status: "queued", ID: dbutil.ID(), AgentID: a.ID, ChatJID: chat, Kind: kind, Text: text, ReplyTo: reply, CreatedAt: s.Now().UTC(), ExpiresAt: s.Now().UTC().Add(30 * time.Minute), State: "pending"}
	if kind == "file" {
		var at model.Attachment
		if e := s.Repo.Get("attachments", attachment, &at); e != nil {
			return ap, model.ErrNotFound
		}
		ap.Attachment = &at
	}
	if e := s.Repo.Put("approvals", ap.ID, ap); e != nil {
		return ap, e
	}
	return ap, nil
}
func (s *Service) Approvals() ([]model.Approval, error) {
	if e := s.Expire(); e != nil {
		return nil, e
	}
	xs, e := List[model.Approval](s.Repo, "approvals")
	sort.Slice(xs, func(i, j int) bool { return xs[i].CreatedAt.After(xs[j].CreatedAt) })
	return xs, e
}
func (s *Service) Expire() error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	xs, e := List[model.Approval](s.Repo, "approvals")
	if e != nil {
		return e
	}
	for _, a := range xs {
		if a.State == "pending" && !s.Now().Before(a.ExpiresAt) {
			a.State = "expired"
			if e = s.Repo.Put("approvals", a.ID, a); e != nil {
				return e
			}
			if e = s.recordUnlocked(model.Activity{AgentID: a.AgentID, Tool: "approval", Kind: "expired", ChatJIDs: []string{a.ChatJID}}); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) Decide(ctx context.Context, id string, approve bool, text *string, reason string) (model.Approval, error) {
	if approve {
		done, err := s.beginOutbound()
		if err != nil {
			return model.Approval{}, err
		}
		defer done()
	}
	if _, ok := HandoffIdentity(ctx); ok {
		return model.Approval{}, model.ErrForbidden
	}
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var a model.Approval
	if e := s.Repo.Get("approvals", id, &a); e != nil {
		return a, e
	}
	if a.State != "pending" {
		return a, model.ErrConflict
	}
	if !s.Now().Before(a.ExpiresAt) {
		a.State = "expired"
		_ = s.Repo.Put("approvals", id, a)
		_ = s.recordUnlocked(model.Activity{AgentID: a.AgentID, Tool: "approval", Kind: "expired", ChatJIDs: []string{a.ChatJID}})
		return a, model.ErrConflict
	}
	if !approve {
		a.State = "denied"
		a.Reason = reason
		if e := s.Repo.Put("approvals", id, a); e != nil {
			return a, e
		}
		return a, s.recordUnlocked(model.Activity{AgentID: a.AgentID, Tool: "approval", Kind: "denied", ChatJIDs: []string{a.ChatJID}})
	}
	agent, e := s.Agent(a.AgentID)
	if e != nil || !agent.Enabled || !agent.Permissions.RequestSend || !s.CanChat(&agent, a.ChatJID) {
		return a, model.ErrForbidden
	}
	if _, _, e := s.sendAgent(agent.ID, a.ChatJID); e != nil {
		return a, e
	}
	if text != nil {
		if len(*text) > 65536 || (*text == "" && a.Kind == "message") {
			return a, model.ErrInvalid
		}
		a.Text = *text
		a.Edited = true
	}
	if s.Messenger == nil {
		return a, model.ErrConflict
	}
	// Persist the irreversible decision before calling the network. After a crash
	// approved/edited rows are never automatically retried (avoid duplicate sends).
	a.State = "approved"
	if a.Edited {
		a.State = "edited"
	}
	if e = s.Repo.Put("approvals", id, a); e != nil {
		return a, e
	}
	reserved, e := s.prepareAuthor(a.AgentID, a.ChatJID)
	if e != nil {
		return a, e
	}
	if e = ctx.Err(); e != nil {
		return a, e
	}
	a.MessageID = reserved
	messageID, e := s.Messenger.Send(ctx, a)
	if e != nil {
		a.State = "failed"
		a.Status = "failed"
		a.Reason = "Delivery failed; review before requesting again."
	} else {
		a.State = "sent"
		a.MessageID = messageID
		a.Status = "sent"
		at := s.Now().UTC()
		a.SentAt = &at
	}
	if err := s.Repo.Put("approvals", id, a); err != nil {
		return a, err
	}
	if a.State == "sent" {
		if err := s.recordOutbound(context.Background(), model.Draft{AgentID: a.AgentID, ChatJID: a.ChatJID, MessageID: messageID}); err != nil {
			return a, err
		}
	}
	act := model.Activity{AgentID: a.AgentID, Tool: "approval", Kind: a.State, ChatJIDs: []string{a.ChatJID}, Count: 1}
	if a.State == "sent" {
		act.Note = a.Text
		act.MessageIDs = []string{messageID}
	}
	return a, s.recordUnlocked(act)
}
func (s *Service) Record(a model.Activity) error { return s.recordUnlocked(a) }
func (s *Service) recordUnlocked(a model.Activity) error {
	s.shared().activityMu.Lock()
	defer s.shared().activityMu.Unlock()
	if a.At.IsZero() {
		a.At = s.Now().UTC()
	}
	if a.ChatJIDs == nil {
		a.ChatJIDs = []string{}
	}
	if a.MessageIDs == nil {
		a.MessageIDs = []string{}
	}
	r, e := s.Repo.DB.Exec("INSERT INTO activity(at,data) VALUES(?,?)", a.At, dbutil.JSON(a))
	if e != nil {
		return e
	}
	a.ID, _ = r.LastInsertId()
	for ch := range s.shared().watchers {
		select {
		case ch <- a:
		default:
		}
	}
	return nil
}
func (s *Service) Subscribe() (chan model.Activity, func()) {
	s.shared().activityMu.Lock()
	defer s.shared().activityMu.Unlock()
	ch := make(chan model.Activity, 64)
	s.shared().watchers[ch] = true
	return ch, func() { s.shared().activityMu.Lock(); delete(s.shared().watchers, ch); s.shared().activityMu.Unlock() }
}

type ActivityFilter struct {
	Agent, Kind, Chat string
	Before            int64
	Limit             int
}

func (s *Service) Activity(a *model.Agent, f ActivityFilter) ([]model.Activity, error) {
	allowed := s.ChatAccess(a)
	rows, e := s.Repo.DB.Query("SELECT id,data FROM activity WHERE (?=0 OR id<?) ORDER BY id DESC", f.Before, f.Before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Activity{}
	skipped := 0
	defer func() {
		if skipped > 0 {
			log.Printf("GET /v1/activity skipped %d unreadable rows", skipped)
		}
	}()
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	for rows.Next() {
		var id int64
		var raw string
		var v model.Activity
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		if e = decode(raw, &v); e != nil {
			skipped++
			continue
		}
		v.ID = id
		if v.ChatJIDs == nil {
			v.ChatJIDs = []string{}
		}
		if v.MessageIDs == nil {
			v.MessageIDs = []string{}
		}
		if f.Agent != "" && v.AgentID != f.Agent || f.Kind != "" && v.Kind != f.Kind || f.Chat != "" && !model.Contains(v.ChatJIDs, f.Chat) {
			continue
		}
		if a != nil {
			if v.AgentID != a.ID {
				continue
			}
			ok := true
			for _, jid := range v.ChatJIDs {
				if !allowed(jid) {
					ok = false
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, v)
		if len(out) >= f.Limit {
			break
		}
	}
	return out, rows.Err()
}
func (s *Service) Retain() error {
	_, e := s.Repo.DB.Exec("DELETE FROM activity WHERE julianday(at)<julianday(?)", s.Now().AddDate(0, 0, -s.Settings().RetentionDays))
	if e != nil {
		return e
	}
	_, e = s.Repo.DB.Exec("DELETE FROM agent_updates WHERE julianday(at)<julianday(?)", s.Now().AddDate(0, 0, -s.Settings().RetentionDays))
	return e
}
func (s *Service) Attachment(data []byte, name string) (model.Attachment, error) {
	if len(data) == 0 || len(data) > 20<<20 {
		return model.Attachment{}, model.ErrInvalid
	}
	at := model.Attachment{ID: dbutil.ID(), Name: attachments.Name(name), MIME: http.DetectContentType(data), CreatedAt: s.Now().UTC(), Size: int64(len(data)), SHA256: dbutil.Digest(string(data))}
	if at.Name == "." || at.Name == "" {
		at.Name = "attachment"
	}
	s.PruneOutbox()
	dir := filepath.Join(s.Home, "outbox")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return at, e
	}
	if e := os.WriteFile(filepath.Join(dir, at.ID), data, 0600); e != nil {
		return at, e
	}
	return at, s.Repo.Put("attachments", at.ID, at)
}
func (s *Service) PendingCount() int {
	xs, _ := s.Approvals()
	n := 0
	for _, x := range xs {
		if x.State == "pending" {
			n++
		}
	}
	return n
}

// ValidAgentName is shared by the admin rename contract.
func ValidAgentName(name string) (string, error) {
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", model.ErrInvalid
		}
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 {
		return "", model.ErrInvalid
	}
	return name, nil
}
