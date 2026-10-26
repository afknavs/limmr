package hub

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"strings"
	"time"
	"unicode/utf8"
)

type backgroundKey struct{}

func WithBackground(ctx context.Context, background bool) context.Context {
	return context.WithValue(ctx, backgroundKey{}, background)
}
func Background(ctx context.Context, a model.Agent) bool {
	v, _ := ctx.Value(backgroundKey{}).(bool)
	return v || a.Name == "limmr-nightly"
}

// SendOptions carries the owner's quoted request, not a change to permissions.
type SendOptions struct {
	TaskID           string
	DraftOnly        bool
	OwnerInstruction string
	QueueForApproval bool
	AsDocument       bool
}

func sendOptions(options []SendOptions) (SendOptions, error) {
	var o SendOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.OwnerInstruction != "" && (strings.TrimSpace(o.OwnerInstruction) == "" || utf8.RuneCountInString(o.OwnerInstruction) > 500) {
		return o, model.ErrInvalid
	}
	return o, nil
}
func instructed(a model.Agent, d model.Draft) bool {
	return a.Preset == "own" && d.OwnerInstruction != ""
}
func parkForReview(ctx context.Context, a model.Agent, d model.Draft) bool {
	if d.QueueForApproval || d.Rule == "ask_in_limmr" {
		return true
	}
	if instructed(a, d) {
		return false
	}
	if a.Preset == "own" {
		return Background(ctx, a)
	}
	// Given agents retain the existing, stricter confirmation behavior.
	return Background(ctx, a) && d.Rule == "confirm" || a.Name == "limmr-nightly"
}

// SendPolicy uses canonical identifiers and only confirmed circles. Uncircled
// chats require confirmation.
func (s *Service) SendPolicy(chat string) (string, error) {
	xs, e := List[model.Circle](s.Repo, "circles")
	if e != nil {
		return "", e
	}
	aliases, e := s.Messages.Aliases()
	if e != nil {
		return "", e
	}
	canonical := func(j string) string {
		if p := aliases[j]; p != "" {
			return p
		}
		return j
	}
	rule, found := "free", false
	for _, c := range xs {
		if !c.Confirmed {
			continue
		}
		for _, j := range c.Chats {
			if canonical(j.ChatJID) != canonical(chat) {
				continue
			}
			found = true
			if c.SendRule == "never" {
				rule = "never"
			} else if c.SendRule == "ask_in_limmr" && rule != "never" {
				rule = "ask_in_limmr"
			} else if c.SendRule != "free" && rule != "never" && rule != "ask_in_limmr" {
				rule = "confirm"
			}
		}
	}
	if !found {
		return "confirm", nil
	}
	return rule, nil
}
func (s *Service) sendAgent(id, chat string) (model.Agent, string, error) {
	a, e := s.Agent(id)
	if e != nil || !a.Enabled || !a.Permissions.RequestSend {
		return a, "", model.ErrForbidden
	}
	cs, _, err := s.Visible(&a)
	if err != nil {
		return a, "", err
	}
	exists := false
	for _, c := range cs {
		if c.JID == chat {
			exists = true
		}
	}
	if !exists || !s.CanChat(&a, chat) {
		return a, "", model.ErrNotFound
	}
	rule, e := s.AgentSendPolicy(a, chat)
	if e != nil {
		return a, "", e
	}
	if rule == "never" {
		return a, "", model.ErrForbidden
	}
	return a, rule, nil
}
func (s *Service) DraftMessage(ctx context.Context, a model.Agent, chat, text, voice string, sendNow bool, options ...SendOptions) (model.Draft, error) {
	return s.DraftMessageVariations(ctx, a, chat, text, voice, sendNow, nil, options...)
}
func (s *Service) DraftMessageVariations(ctx context.Context, a model.Agent, chat, text, voice string, sendNow bool, variations []string, options ...SendOptions) (model.Draft, error) {
	if e := ValidateVariations(variations); e != nil {
		return model.Draft{}, e
	}
	return s.draftSend(ctx, a, "message", chat, text, voice, "", "", sendNow, variations, "", options...)
}
func (s *Service) DraftSend(ctx context.Context, a model.Agent, kind, chat, text, voice, reply, attachment string, sendNow bool, options ...SendOptions) (model.Draft, error) {
	return s.draftSend(ctx, a, kind, chat, text, voice, reply, attachment, sendNow, nil, "", options...)
}
func (s *Service) draftSend(ctx context.Context, a model.Agent, kind, chat, text, _, reply, attachment string, sendNow bool, variations []string, key string, options ...SendOptions) (model.Draft, error) {
	done, err := s.beginOutbound()
	if err != nil {
		return model.Draft{}, err
	}
	defer done()
	o, e := sendOptions(options)
	if e != nil {
		return model.Draft{}, e
	}
	var resolveErr error
	chat, resolveErr = s.ResolveChat(chat)
	if resolveErr != nil {
		return model.Draft{}, resolveErr
	}
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if len([]rune(key)) > 128 {
		return model.Draft{}, model.ErrInvalid
	}
	if key != "" {
		current, err := s.Agent(a.ID)
		if err != nil || !current.Enabled || !current.Permissions.RequestSend {
			return model.Draft{}, model.ErrForbidden
		}
		if err := s.CheckAccess(current); err != nil {
			return model.Draft{}, err
		}
		current.RunChat = a.RunChat
		a = current
		if previous, e := s.keyedDraft(a.ID, key); e == nil {
			if !s.CanChat(&a, previous.ChatJID) || a.RunChat != "" && previous.ChatJID != a.RunChat {
				return model.Draft{}, model.ErrNotFound
			}
			return previous, nil
		} else if e != model.ErrNotFound {
			return model.Draft{}, e
		}
	}
	a, rule, e := s.sendAgent(a.ID, chat)
	if e != nil {
		return model.Draft{}, e
	}
	if !model.Contains([]string{"message", "file", "reaction"}, kind) || len(text) > 65536 || kind == "message" && strings.TrimSpace(text) == "" || kind == "reaction" && reply == "" {
		return model.Draft{}, model.ErrInvalid
	}
	if reply != "" {
		var n int
		e = s.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=? AND id=?", chat, reply).Scan(&n)
		if e != nil {
			return model.Draft{}, e
		}
		if n != 1 {
			return model.Draft{}, model.ErrNotFound
		}
	}
	d := model.Draft{ID: dbutil.ID(), AgentID: a.ID, ChatJID: chat, FinalText: text, Rule: rule, NeedsConfirmation: rule != "free", CreatedAt: s.Now().UTC(), ExpiresAt: s.Now().UTC().Add(30 * time.Minute), State: "draft", Kind: kind, ReplyTo: reply}
	d.TaskID = o.TaskID
	if d.TaskID != "" {
		if s.TaskSendValidate == nil {
			return model.Draft{}, model.ErrConflict
		}
		if err := s.TaskSendValidate(a, d.TaskID, chat); err != nil {
			return model.Draft{}, err
		}
	}
	d.AsDocument = o.AsDocument
	d.OwnerInstruction = o.OwnerInstruction
	if a.Preset != "own" {
		d.OwnerInstruction = ""
	}
	d.QueueForApproval = o.QueueForApproval
	d.NeedsConfirmation = rule != "free" && !instructed(a, d) || d.QueueForApproval || rule == "ask_in_limmr"
	d.Key = key
	d.Status = "queued"
	if rule == "confirm" {
		d.ApprovalID = d.ID
	}
	d.Variations = variations
	if kind == "file" {
		var at model.Attachment
		if e = s.Repo.Get("attachments", attachment, &at); e != nil {
			return d, e
		}
		d.Attachment = &at
	}
	if identity, ok := HandoffIdentity(ctx); ok {
		d.HandoffID = identity.HandoffID
		d.RunID = identity.RunID
		mode, err := s.handoffPolicy(ctx, d, false)
		if err != nil {
			return model.Draft{}, err
		}
		if mode == "park" {
			return s.parkDraft(d, "Handoff quiet hours or reply budget requires review.")
		}
	}
	if o.DraftOnly {
		return d, s.Repo.Put("drafts", d.ID, d)
	}
	if parkForReview(ctx, a, d) {
		return s.parkDraft(d, "Draft requires your review in Limmr.")
	}

	if e = s.Repo.Put("drafts", d.ID, d); e != nil {
		return d, e
	}
	if instructed(a, d) {
		return s.deliverDraft(ctx, d, "owner_instruction")
	}
	if sendNow && rule == "free" {
		return s.deliverDraft(ctx, d, "free")
	}
	return d, nil
}
func (s *Service) SendDraft(ctx context.Context, a model.Agent, id string, options ...SendOptions) (model.Draft, error) {
	done, err := s.beginOutbound()
	if err != nil {
		return model.Draft{}, err
	}
	defer done()
	o, e := sendOptions(options)
	if e != nil {
		return model.Draft{}, e
	}
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if id == "" {
		return model.Draft{}, model.ErrInvalid
	}
	var d model.Draft
	if e := s.Repo.Get("drafts", id, &d); e != nil {
		return d, e
	}
	if d.AgentID != a.ID {
		return model.Draft{}, model.ErrNotFound
	}
	current, rule, e := s.sendAgent(a.ID, d.ChatJID)
	if e != nil {
		return model.Draft{}, e
	}
	if o.TaskID != "" && o.TaskID != d.TaskID {
		return d, model.ErrInvalid
	}
	if d.TaskID != "" {
		if s.TaskSendValidate == nil {
			return d, model.ErrConflict
		}
		if err := s.TaskSendValidate(current, d.TaskID, d.ChatJID); err != nil {
			return d, err
		}
	}
	if d.State != "draft" || !s.Now().Before(d.ExpiresAt) {
		return d, model.ErrConflict
	}

	d.Rule = rule
	if current.Preset != "own" {
		d.OwnerInstruction = ""
	} else if o.OwnerInstruction != "" {
		d.OwnerInstruction = o.OwnerInstruction
	}
	d.QueueForApproval = d.QueueForApproval || o.QueueForApproval
	d.NeedsConfirmation = rule != "free" && !instructed(current, d) || d.QueueForApproval || rule == "ask_in_limmr"
	if mode, err := s.handoffPolicy(ctx, d, false); err != nil {
		return model.Draft{}, err
	} else if mode == "park" {
		return s.parkDraft(d, "Handoff quiet hours or reply budget requires review.")
	}
	if parkForReview(ctx, current, d) {
		return s.parkDraft(d, "Draft requires your review in Limmr.")
	}
	if instructed(current, d) {
		return s.deliverDraft(ctx, d, "owner_instruction")
	}

	if rule == "confirm" && s.Now().Sub(d.CreatedAt) < 2*time.Second {
		return d, model.ErrConflict
	}
	via := "free"
	if rule == "confirm" {
		via = "chat"
	}
	return s.deliverDraft(ctx, d, via)
}

// Caller holds mu. Persist the irreversible transition before invoking Messenger.
func (s *Service) deliverDraft(ctx context.Context, d model.Draft, via string) (model.Draft, error) {
	if s.Messenger == nil {
		return d, model.ErrConflict
	}
	if mode, err := s.handoffPolicy(ctx, d, true); err != nil {
		return d, err
	} else if mode == "park" {
		return s.parkDraft(d, "Handoff quiet hours or reply budget requires review.")
	}
	d.State = "sending"
	d.Status = "queued"
	d.ConfirmedVia = via
	if e := s.Repo.Put("drafts", d.ID, d); e != nil {
		return d, e
	}
	reserved, e := s.prepareAuthor(d.AgentID, d.ChatJID)
	if e != nil {
		return d, e
	}
	if e = ctx.Err(); e != nil {
		return d, e
	}
	id, e := s.Messenger.Send(ctx, model.Approval{AsDocument: d.AsDocument, MessageID: reserved, ID: d.ID, AgentID: d.AgentID, ChatJID: d.ChatJID, Kind: d.Kind, Text: d.FinalText, Attachment: d.Attachment, ReplyTo: d.ReplyTo, State: "approved"})
	if e != nil {
		d.State = "failed"
		d.Status = "failed"
	} else {
		d.State = "sent"
		d.MessageID = id
		d.Status = "sent"
		at := s.Now().UTC()
		d.SentAt = &at
	}
	if e = s.Repo.Put("drafts", d.ID, d); e != nil {
		return d, e
	}
	if d.State == "sent" {
		if s.TaskSent != nil {
			if err := s.TaskSent(d); err != nil {
				return d, err
			}
		}
		if err := s.recordOutbound(context.Background(), d); err != nil {
			return d, err
		}
	}
	act := model.Activity{AgentID: d.AgentID, Tool: "send_draft", Kind: d.State, ChatJIDs: []string{d.ChatJID}, Count: 1, Note: d.FinalText, ConfirmedVia: via, OwnerInstruction: d.OwnerInstruction, QueueForApproval: d.QueueForApproval}
	act.Description = "sent by integration"
	if via == "mac" {
		act.Description = "sent by you from Limmr"
		if d.State != "sent" {
			act.Description = "send failed from Limmr"
		}
		act.DraftedBy = d.DraftedBy
		act.Edited = &d.Edited
		act.Variation = &d.Variation
	}
	if d.MessageID != "" {
		act.MessageIDs = []string{d.MessageID}
	}
	return d, s.recordUnlocked(act)
}
func (s *Service) Suggestions() ([]model.Suggestion, error) {
	xs, e := List[model.Suggestion](s.Repo, "suggestions")
	if e != nil {
		return nil, e
	}
	out := []model.Suggestion{}
	for _, x := range xs {
		if x.State == "pending" && s.Now().Before(x.ExpiresAt) {
			x, e = s.PresentSuggestion(x)
			if e != nil {
				return nil, e
			}
			out = append(out, x)
		}
	}
	sortSuggestions(out)
	return out, e
}
func (s *Service) DismissSuggestion(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var x model.Suggestion
	if e := s.Repo.Get("suggestions", id, &x); e != nil {
		return e
	}
	if x.State != "pending" {
		return model.ErrConflict
	}
	x.State = "dismissed"
	if err := s.Repo.Put("suggestions", id, x); err != nil {
		return err
	}
	if s.TaskSuggestionDone != nil {
		return s.TaskSuggestionDone(x)
	}
	return nil
}
func (s *Service) SendSuggestion(ctx context.Context, id string, text *string) (model.Draft, error) {
	return s.SendSuggestionOptions(ctx, id, SuggestionSend{Text: text})
}
func (s *Service) SendSuggestionOptions(ctx context.Context, id string, options SuggestionSend) (model.Draft, error) {
	done, err := s.beginOutbound()
	if err != nil {
		return model.Draft{}, err
	}
	defer done()
	ctx = context.WithValue(ctx, ownerReviewKey{}, true)
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var x model.Suggestion
	if e := s.Repo.Get("suggestions", id, &x); e != nil {
		return model.Draft{}, e
	}
	if (x.Kind != "reply" && x.Kind != "check_in" && x.Kind != "nudge") || x.ChatJID == "" || x.State != "pending" || !s.Now().Before(x.ExpiresAt) {
		return model.Draft{}, model.ErrConflict
	}
	var d model.Draft
	if x.DraftID != "" {
		if e := s.Repo.Get("drafts", x.DraftID, &d); e != nil {
			return d, e
		}
		if d.State != "suggested" {
			return d, model.ErrConflict
		}
	} else {
		d = model.Draft{ID: dbutil.ID(), AgentID: x.CreatedBy, ChatJID: x.ChatJID, FinalText: x.DraftText, CreatedAt: s.Now().UTC(), ExpiresAt: x.ExpiresAt, Kind: "message", SuggestionID: x.ID}
	}
	d.TaskID = x.TaskID
	a, rule, e := s.ownerSendAgent(d.AgentID, d.ChatJID)
	if e != nil {
		return model.Draft{}, e
	}
	if d.TaskID != "" {
		if s.TaskSendValidate == nil {
			return d, model.ErrConflict
		}
		if err := s.TaskSendValidate(a, d.TaskID, d.ChatJID); err != nil {
			return d, err
		}
	}
	x, e = s.PresentSuggestion(x)
	if e != nil {
		return d, e
	}
	index := 0
	if options.Variation != nil {
		index = *options.Variation
	}
	if index < 0 || index >= len(x.Variations) {
		return d, model.ErrInvalid
	}
	body := x.Variations[index]
	if options.Text != nil {
		body = *options.Text
	}
	d.Rule = rule
	d.NeedsConfirmation = rule == "confirm"
	d.Variation = index
	d.Edited = body != x.Variations[index]
	d.DraftedBy = a.Name
	d.FinalText = body
	if d.Kind == "message" && strings.TrimSpace(body) == "" || len(d.FinalText) > 65536 {
		return d, model.ErrInvalid
	}
	if s.Messenger == nil {
		return d, model.ErrConflict
	}
	if mode, err := s.handoffPolicy(ctx, d, false); err != nil {
		return d, err
	} else if mode == "park" {
		return d, model.ErrConflict
	}
	// Consume suggestion first; a crash must never enable a duplicate delivery.
	x.State = "sending"
	if e = s.Repo.Put("suggestions", x.ID, x); e != nil {
		return d, e
	}
	d, e = s.deliverDraft(ctx, d, "mac")
	if e != nil {
		return d, e
	}
	x.State = d.State
	return d, s.Repo.Put("suggestions", x.ID, x)
}

func (s *Service) circleForChat(chat string) string {
	cs, e := List[model.Circle](s.Repo, "circles")
	if e != nil {
		return ""
	}
	for _, c := range cs {
		if !c.Confirmed {
			continue
		}
		for _, j := range c.Chats {
			if j.ChatJID == chat {
				return c.ID
			}
		}
	}
	return ""
}
