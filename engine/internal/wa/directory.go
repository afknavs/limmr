package wa

import (
	"context"
	"fmt"
	"go.mau.fi/whatsmeow/types"
	"limmr/internal/model"
	"strings"
	"time"
)

// Load the copied session's contacts and legacy group cache before connecting.
// This reads only Limmr's own session store, never a source session path.
func (l *Live) loadDirectory(ctx context.Context) error {
	if e := l.normalizeStoredLIDs(ctx); e != nil {
		return e
	}
	if l.Directory == nil {
		return nil
	}
	contacts, e := l.Client.Store.Contacts.GetAllContacts(ctx)
	if e != nil {
		return fmt.Errorf("contact cache unavailable")
	}
	for jid, c := range contacts {
		canonical := l.resolve(ctx, jid, types.EmptyJID)
		// A LID push-name must not race the phone contact's saved name during
		// map iteration. Prefer the resolved phone contact when it is cached.
		if canonical != jid.ToNonAD() {
			if phoneContact, ok := contacts[canonical]; ok {
				if phoneContact.PushName == "" {
					phoneContact.PushName = c.PushName
				}
				c = phoneContact
			}
		}
		name := c.FullName
		if name == "" {
			name = c.FirstName
		}
		if name == "" {
			name = c.PushName
		}
		if name == "" {
			name = c.BusinessName
		}
		if name == "" {
			name = model.FallbackName(canonical.String())
		}
		aliases := []string{canonical.String()}
		if canonical != jid.ToNonAD() {
			aliases = append(aliases, jid.ToNonAD().String())
		}
		if e = l.Directory.PutPerson(model.Person{ID: model.PersonID(canonical.String()), JIDs: aliases, Name: name, Business: c.BusinessName != "", Broadcast: canonical.Server == "newsletter" || canonical.Server == "broadcast", PushName: c.PushName, UpdatedAt: time.Now().UTC()}); e != nil {
			return e
		}
	}
	var tables int
	if e = l.Messages.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('wa_groups','wa_group_participants')").Scan(&tables); e != nil {
		return e
	}
	if tables != 2 {
		return nil
	}
	rows, e := l.Messages.DB.Query("SELECT jid,COALESCE(name,''),COALESCE(topic,''),COALESCE(participant_count,0) FROM wa_groups")
	if e != nil {
		return e
	}
	groups := []model.Group{}
	for rows.Next() {
		var g model.Group
		if e = rows.Scan(&g.JID, &g.Name, &g.Description, &g.MemberCount); e != nil {
			rows.Close()
			return e
		}
		groups = append(groups, g)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	rows, e = l.Messages.DB.Query("SELECT group_jid,jid,COALESCE(phone_jid,''),COALESCE(lid,''),COALESCE(is_admin,0),COALESCE(is_super_admin,0) FROM wa_group_participants")
	if e != nil {
		return e
	}
	type row struct {
		group, jid, pn, lid string
		admin, super        bool
	}
	members := []row{}
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.group, &r.jid, &r.pn, &r.lid, &r.admin, &r.super); e != nil {
			rows.Close()
			return e
		}
		members = append(members, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	ms := []model.Membership{}
	for _, r := range members {
		lid, _ := types.ParseJID(r.lid)
		pn, _ := types.ParseJID(r.pn)
		if lid.Server == types.HiddenUserServer && pn.Server == types.DefaultUserServer {
			l.normalize(lid.ToNonAD(), pn.ToNonAD())
		}
		raw, e := types.ParseJID(r.jid)
		if e != nil {
			continue
		}
		alt, _ := types.ParseJID(r.pn)
		canonical := l.resolve(ctx, raw, alt)
		l.person(ctx, canonical, "")
		admin := r.admin || r.super
		ms = append(ms, model.Membership{Group: r.group, Person: model.PersonID(canonical.String()), Admin: admin})
		if l.Client.Store.ID != nil && canonical == l.Client.Store.ID.ToNonAD() && admin {
			for i := range groups {
				if groups[i].JID == r.group {
					groups[i].IAmAdmin = true
				}
			}
		}
	}
	if sink, ok := l.Directory.(interface {
		ReplaceGroups(context.Context, []model.Group, []model.Membership) error
	}); ok {
		return sink.ReplaceGroups(ctx, groups, ms)
	}
	return nil
}

// Normalize legacy chats and group senders even if they have no contact entry.
// Query only our own copied message/session stores, before any network connect.
func (l *Live) normalizeStoredLIDs(ctx context.Context) error {
	rows, err := l.Messages.DB.Query("SELECT jid FROM chats WHERE jid LIKE '%@lid' UNION SELECT sender FROM messages WHERE sender LIKE '%@lid' OR sender NOT LIKE '%@%'")
	if err != nil {
		return err
	}
	js := []types.JID{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if !strings.Contains(raw, "@") {
			raw += "@lid"
		}
		if j, e := types.ParseJID(raw); e == nil {
			js = append(js, j)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range js {
		l.resolve(ctx, j, types.EmptyJID)
	}
	return nil
}
