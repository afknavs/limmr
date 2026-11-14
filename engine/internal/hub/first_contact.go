package hub

import (
	"context"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/wa"
	"regexp"
	"strings"
	"time"
	"unicode"
)

func phoneJID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "@s.whatsapp.net") {
		value = strings.TrimSuffix(value, "@s.whatsapp.net")
	}
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		} else if !strings.ContainsRune("+ ()-.", r) {
			return ""
		}
	}
	if digits.Len() < 7 || digits.Len() > 15 || digits.String()[0] == '0' {
		return ""
	}
	return digits.String() + "@s.whatsapp.net"
}

func (s *Service) contactAddress(p model.Person) (string, error) {
	aliases, err := s.Messages.Aliases()
	if err != nil {
		return "", err
	}
	hidden := s.HiddenChatAccess()
	address := ""
	for _, j := range p.JIDs {
		if hidden(j) {
			return "", model.ErrNotFound
		}
		if aliases[j] != "" {
			j = aliases[j]
		}
		if hidden(j) {
			return "", model.ErrNotFound
		}
		if phone := phoneJID(j); phone != "" {
			address = phone
		}
	}
	if address != "" {
		return address, nil
	}
	for _, j := range p.JIDs {
		if strings.HasSuffix(j, "@lid") {
			return j, nil
		}
	}
	return "", model.ErrNotFound
}

// ContactMember exposes only the directory identity and role from a readable group.
// Private profile details and unrelated memberships are never included.
func (s *Service) ContactMember(a model.Agent, id string) (model.FirstContactInfo, error) {
	if !a.Enabled || !a.Permissions.ReadMessages || !a.Permissions.ReadPeople || a.Permissions.Access.Ended(s.Now()) {
		return model.FirstContactInfo{}, model.ErrNotFound
	}
	var p model.Person
	if err := s.Repo.Get("people", id, &p); err != nil {
		return model.FirstContactInfo{}, err
	}
	address, err := s.contactAddress(p)
	if err != nil {
		return model.FirstContactInfo{}, err
	}
	groups, err := List[model.Membership](s.Repo, "memberships")
	if err != nil {
		return model.FirstContactInfo{}, err
	}
	_, visible, err := s.Visible(&a)
	if err != nil {
		return model.FirstContactInfo{}, err
	}
	for _, m := range groups {
		if m.Person != id || !model.Contains(visible, m.Group) || a.RunChat != "" && a.RunChat != m.Group {
			continue
		}
		var g model.Group
		if err := s.Repo.Get("groups", m.Group, &g); err != nil {
			if err == model.ErrNotFound {
				continue
			}
			return model.FirstContactInfo{}, err
		}
		name := p.Name
		if !model.Named(model.Person{Name: name}) {
			name = p.PushName
		}
		if !model.Named(model.Person{Name: name}) {
			name = "Unnamed person"
		}
		cp := &model.ContactPerson{ID: p.ID, Name: name, Role: "member"}
		if m.Admin {
			cp.Role = "admin"
		}
		if phone := phoneJID(address); phone != "" {
			cp.Phone = "+" + strings.TrimSuffix(phone, "@s.whatsapp.net")
		}
		return model.FirstContactInfo{FirstContact: true, FirstContactPerson: cp, FirstContactGroup: &model.ContactGroup{ID: g.JID, Name: g.Name}}, nil
	}
	return model.FirstContactInfo{}, model.ErrNotFound
}

func quoteNamesContact(quote string, info model.FirstContactInfo) bool {
	p := info.FirstContactPerson
	if p == nil || strings.TrimSpace(quote) == "" {
		return false
	}
	if p.Phone != "" {
		wanted := phoneJID(p.Phone)
		for _, number := range regexp.MustCompile(`[+]?[0-9][0-9 ().-]{5,}[0-9]`).FindAllString(quote, -1) {
			if phoneJID(number) == wanted {
				return true
			}
		}
	}
	if p.ID == "" || p.Name == "Unnamed person" {
		return false
	}
	words := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	for _, part := range words(p.Name) {
		// Initials and two-letter particles ("A", "de") match ordinary words in any sentence,
		// so they never count as the owner naming this person.
		if len([]rune(part)) < 3 {
			continue
		}
		for _, word := range words(quote) {
			if word == part {
				return true
			}
		}
	}
	return false
}

// sendRecipient first preserves the ordinary visible-chat path, then admits a
// bounded first contact. Unknown and ineligible recipients share ErrNotFound.
func (s *Service) sendRecipient(ctx context.Context, requested model.Agent, recipient, kind, reply string, o SendOptions) (model.Agent, string, string, model.FirstContactInfo, error) {
	empty := model.FirstContactInfo{}
	a, err := s.Agent(requested.ID)
	if err != nil || !a.Enabled || !a.Permissions.RequestSend {
		return a, "", "", empty, model.ErrForbidden
	}
	if err := s.CheckAccess(a); err != nil {
		return a, "", "", empty, err
	}
	a.RunChat = requested.RunChat
	address := recipient
	var person model.Person
	err = s.Repo.Get("people", recipient, &person)
	if err != nil && err != model.ErrNotFound {
		return a, "", "", empty, err
	}
	people, err := List[model.Person](s.Repo, "people")
	if err != nil {
		return a, "", "", empty, err
	}
	if person.ID == "" {
		pn := phoneJID(recipient)
		for _, p := range people {
			for _, j := range p.JIDs {
				if j == recipient || pn != "" && phoneJID(j) == pn {
					person = p
					break
				}
			}
			if person.ID != "" {
				break
			}
		}
	}
	if person.ID != "" {
		address, err = s.contactAddress(person)
		if err != nil {
			return a, "", "", empty, err
		}
	} else if pn := phoneJID(recipient); pn != "" {
		address = pn
	}
	aliases, err := s.Messages.Aliases()
	if err != nil {
		return a, "", "", empty, err
	}
	if aliases[address] != "" {
		address = aliases[address]
	}
	chats, err := s.Messages.Chats()
	if err != nil {
		return a, "", "", empty, err
	}
	for _, c := range chats {
		canonical := c.JID
		if aliases[canonical] != "" {
			canonical = aliases[canonical]
		}
		if canonical == address {
			if a.RunChat != "" && a.RunChat != canonical {
				return a, "", "", empty, model.ErrNotFound
			}
			current, rule, e := s.sendAgent(a.ID, c.JID)
			return current, c.JID, rule, empty, e
		}
	}
	if kind != "message" && kind != "file" || reply != "" || a.RunChat != "" || s.HiddenChat(address) {
		return a, "", "", empty, model.ErrNotFound
	}
	info := empty
	if person.ID != "" {
		info, err = s.ContactMember(a, person.ID)
		if err != nil && err != model.ErrNotFound {
			return a, "", "", empty, err
		}
	}
	if !info.FirstContact {
		// Bare numbers are the only exception to membership. Never bypass a hidden
		// known person, or accept a person ID or LID without a readable source group.
		pn := phoneJID(recipient)
		info = model.FirstContactInfo{FirstContact: true, FirstContactPerson: &model.ContactPerson{Name: "+" + strings.TrimSuffix(pn, "@s.whatsapp.net"), Phone: "+" + strings.TrimSuffix(pn, "@s.whatsapp.net")}}
		if pn == "" || a.Preset != "own" || !quoteNamesContact(o.OwnerInstruction, info) {
			return a, "", "", empty, model.ErrNotFound
		}
		address = pn
	}
	rule, err := s.firstContactRule(a, address, info)
	if err != nil {
		return a, "", "", empty, err
	}
	if err := s.firstContactReady(); err != nil {
		return a, "", "", empty, err
	}
	if err := s.checkFirstContactLimit(); err != nil {
		return a, "", "", empty, err
	}
	resolved, err := s.resolveFirstContact(ctx, address)
	if err != nil {
		return a, "", "", empty, err
	}
	if s.HiddenChat(resolved) {
		return a, "", "", empty, model.ErrNotFound
	}
	// Client mappings may reveal a phone alias not yet present in the directory.
	for _, p := range people {
		for _, j := range p.JIDs {
			if j == resolved {
				if _, err := s.contactAddress(p); err != nil {
					return a, "", "", empty, err
				}
			}
		}
	}
	for _, c := range chats {
		j := c.JID
		if aliases[j] != "" {
			j = aliases[j]
		}
		if j == resolved {
			return a, "", "", empty, model.ErrNotFound
		}
	}
	info.FirstContactPerson.Phone = "+" + strings.TrimSuffix(resolved, "@s.whatsapp.net")
	return a, resolved, rule, info, nil
}

func (s *Service) firstContactRule(a model.Agent, chat string, info model.FirstContactInfo) (string, error) {
	rule, err := s.AgentSendPolicy(a, chat)
	if err != nil {
		return "", err
	}
	if info.FirstContactGroup != nil {
		source, e := s.AgentSendPolicy(a, info.FirstContactGroup.ID)
		if e != nil {
			return "", e
		}
		if source == "never" {
			rule = "never"
		}
		if source == "ask_in_limmr" && rule != "never" {
			rule = source
		}
	}
	if rule == "never" {
		return "", model.ErrForbidden
	}
	if rule != "ask_in_limmr" {
		rule = "confirm"
	}
	return rule, nil
}

func (s *Service) resolveFirstContact(ctx context.Context, address string) (string, error) {
	resolver := s.Recipients
	if resolver == nil {
		resolver, _ = s.Messenger.(wa.RecipientResolver)
	}
	if resolver == nil {
		return "", fmt.Errorf("Phone-number checks are unavailable; reconnect WhatsApp before starting a new chat")
	}
	resolved, err := resolver.ResolveRecipient(ctx, address)
	if err != nil {
		return "", err
	}
	if phoneJID(resolved) != resolved {
		return "", fmt.Errorf("Cannot resolve this member to a phone-number address")
	}
	return resolved, nil
}

func (s *Service) checkFirstContactLimit() error {
	var n int
	// Persistent reservations survive crashes and draft/activity retention. Count
	// attempts conservatively: an uncertain acknowledgement may already be sent.
	err := s.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='first_contact_attempts' AND json_extract(data,'$.at_ns')>?`, s.Now().Add(-24*time.Hour).UnixNano()).Scan(&n)
	if err != nil {
		return err
	}
	if n >= *s.Settings().FirstContactLimit {
		return fmt.Errorf("First-contact limit reached (%d per rolling 24 hours); nothing was queued. Wait or ask the owner to change the limit in Limmr", *s.Settings().FirstContactLimit)
	}
	return nil
}

func (s *Service) recheckFirstContact(ctx context.Context, d model.Draft) (model.Agent, error) {
	if err := s.firstContactReady(); err != nil {
		return model.Agent{}, err
	}
	a, err := s.Agent(d.AgentID)
	if err != nil || !a.Enabled || !a.Permissions.RequestSend {
		return a, model.ErrForbidden
	}
	if err := s.CheckAccess(a); err != nil {
		return a, err
	}
	if s.HiddenChat(d.ChatJID) {
		return a, model.ErrNotFound
	}
	if d.TaskID != "" {
		if s.TaskSendValidate == nil {
			return a, model.ErrConflict
		}
		if err := s.TaskSendValidate(a, d.TaskID, d.ChatJID); err != nil {
			return a, err
		}
	}
	if d.FirstContactGroup != nil {
		info, err := s.ContactMember(a, d.FirstContactPerson.ID)
		if err != nil {
			return a, err
		}
		if info.FirstContactGroup.ID != d.FirstContactGroup.ID {
			return a, model.ErrNotFound
		}
		var p model.Person
		if err := s.Repo.Get("people", d.FirstContactPerson.ID, &p); err != nil {
			return a, err
		}
		address, err := s.contactAddress(p)
		if err != nil {
			return a, err
		}
		resolved, err := s.resolveFirstContact(ctx, address)
		if err != nil {
			return a, err
		}
		if resolved != d.ChatJID {
			return a, model.ErrNotFound
		}
	} else {
		// Persisted quotes do not authorize a changed agent preset or hidden alias.
		if a.Preset != "own" || !quoteNamesContact(d.OwnerInstruction, d.FirstContactInfo) {
			return a, model.ErrNotFound
		}
		ps, err := List[model.Person](s.Repo, "people")
		if err != nil {
			return a, err
		}
		for _, p := range ps {
			for _, j := range p.JIDs {
				if j == d.ChatJID {
					if _, err := s.contactAddress(p); err != nil {
						return a, err
					}
				}
			}
		}
		if _, err := s.resolveFirstContact(ctx, d.ChatJID); err != nil {
			return a, err
		}
	}
	if _, err := s.firstContactRule(a, d.ChatJID, d.FirstContactInfo); err != nil {
		return a, err
	}
	return a, s.checkFirstContactLimit()
}

func (s *Service) parkFirstContact(d model.Draft) (model.Draft, error) {
	d.State = "pending"
	d.NeedsConfirmation = true
	d.ApprovalID = d.ID
	d.NextStep = "Approve this new chat in Limmr. Reply access still follows your existing permissions."
	ap := model.Approval{FirstContactInfo: d.FirstContactInfo, ID: d.ID, AgentID: d.AgentID, ChatJID: d.ChatJID, Kind: d.Kind, Text: d.FinalText, Attachment: d.Attachment, AsDocument: d.AsDocument, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, State: "pending", Status: "queued"}
	return d, s.Repo.Apply(Mutation{Bucket: "drafts", ID: d.ID, Value: d}, Mutation{Bucket: "approvals", ID: d.ID, Value: ap})
}

// Status is provenance, not permission to read replies. Recheck the originating
// grant and hidden aliases without contacting WhatsApp for a status query.
func (s *Service) canSeeContactDraft(a model.Agent, d model.Draft) bool {
	current, err := s.Agent(a.ID)
	if err != nil || !current.Enabled || current.Permissions.Access.Ended(s.Now()) || a.RunChat != "" || s.HiddenChat(d.ChatJID) {
		return false
	}
	if d.FirstContactGroup != nil {
		info, err := s.ContactMember(current, d.FirstContactPerson.ID)
		return err == nil && info.FirstContactGroup.ID == d.FirstContactGroup.ID
	}
	if current.Preset != "own" || !quoteNamesContact(d.OwnerInstruction, d.FirstContactInfo) {
		return false
	}
	ps, err := List[model.Person](s.Repo, "people")
	if err != nil {
		return false
	}
	for _, p := range ps {
		for _, j := range p.JIDs {
			if j == d.ChatJID {
				if _, err := s.contactAddress(p); err != nil {
					return false
				}
			}
		}
	}
	return true
}

func (s *Service) decideFirstContact(ctx context.Context, ap model.Approval, approve bool, text *string, reason string) (model.Approval, error) {
	var d model.Draft
	if err := s.Repo.Get("drafts", ap.ID, &d); err != nil {
		return ap, err
	}
	if d.State != "pending" {
		return ap, model.ErrConflict
	}
	if !approve {
		ap.State, ap.Status, ap.Reason = "denied", "failed", reason
		d.State, d.Status = "denied", "failed"
		if err := s.Repo.Apply(Mutation{Bucket: "drafts", ID: d.ID, Value: d}, Mutation{Bucket: "approvals", ID: ap.ID, Value: ap}); err != nil {
			return ap, err
		}
		return ap, s.recordUnlocked(model.Activity{FirstContactInfo: d.FirstContactInfo, AgentID: d.AgentID, Tool: "approval", Kind: "denied", ChatJIDs: []string{d.ChatJID}})
	}
	if text != nil {
		if len(*text) > 65536 || d.Kind == "message" && strings.TrimSpace(*text) == "" {
			return ap, model.ErrInvalid
		}
		d.FinalText = *text
		d.Edited = d.FinalText != ap.Text
	}
	if _, err := s.recheckFirstContact(ctx, d); err != nil {
		return ap, err
	}
	// Consume the approval before the irreversible delivery path. A crash cannot
	// turn a potentially sent message back into an approvable pending card.
	ap.State = "approved"
	if err := s.Repo.Put("approvals", ap.ID, ap); err != nil {
		return ap, err
	}
	d, err := s.deliverDraft(ctx, d, "mac")
	ap.Text, ap.Edited, ap.State, ap.Status = d.FinalText, d.Edited, d.State, d.Status
	ap.MessageID, ap.SentAt, ap.Upload = d.MessageID, d.SentAt, d.Upload
	if saveErr := s.Repo.Put("approvals", ap.ID, ap); saveErr != nil {
		return ap, saveErr
	}
	return ap, err
}

// Paired coordination may deliver later on a different engine, whose local AI
// grant and budget cannot stand in for this originating grant. Do not enqueue
// a first contact until synchronous delivery can keep those checks together.
func (s *Service) firstContactReady() error {
	if gate, ok := s.Messenger.(interface{ FirstContactReady() error }); ok {
		return gate.FirstContactReady()
	}
	return nil
}
