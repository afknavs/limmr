package wa

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// RecipientResolver checks registration and returns a phone-number address.
// It never subscribes to presence, marks read or sends typing notifications.
type RecipientResolver interface {
	ResolveRecipient(context.Context, string) (string, error)
}

func (l *Live) ResolveRecipient(ctx context.Context, address string) (string, error) {
	if !l.Connected() {
		return "", fmt.Errorf("WhatsApp is offline; reconnect before starting a new chat")
	}
	jid, err := types.ParseJID(address)
	if err != nil {
		return "", fmt.Errorf("Cannot resolve a phone number for this member")
	}
	if jid.Server == types.HiddenUserServer {
		jid, err = l.Client.Store.LIDs.GetPNForLID(ctx, jid)
		if err != nil || jid.IsEmpty() {
			return "", fmt.Errorf("This member's phone number is unavailable; refresh the group before trying again")
		}
	}
	if jid.Server != types.DefaultUserServer {
		return "", fmt.Errorf("Cannot resolve a phone number for this member")
	}
	users, err := l.Client.IsOnWhatsApp(ctx, []string{jid.User})
	if err != nil {
		return "", fmt.Errorf("Could not check whether this number is on WhatsApp; try again later")
	}
	if len(users) != 1 || !users[0].IsIn {
		return "", fmt.Errorf("This number is not on WhatsApp")
	}
	// IsOnWhatsApp can return a hidden-number JID with a separate phone address.
	resolved := users[0].PhoneNumber.ToNonAD()
	if resolved.IsEmpty() && users[0].JID.Server == types.DefaultUserServer {
		resolved = users[0].JID.ToNonAD()
	}
	if resolved.Server != types.DefaultUserServer || resolved.User != jid.User {
		return "", fmt.Errorf("WhatsApp could not confirm this member's phone-number address")
	}
	return resolved.String(), nil
}

func (f *Fake) ResolveRecipient(ctx context.Context, address string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.RecipientChecks = append(f.RecipientChecks, address)
	if f.RecipientError != nil {
		return "", f.RecipientError
	}
	if mapped := f.RecipientMappings[address]; mapped != "" {
		address = mapped
	}
	if !strings.HasSuffix(address, "@s.whatsapp.net") {
		return "", fmt.Errorf("This member's phone number is unavailable; refresh the group before trying again")
	}
	if f.NotOnWhatsApp[address] {
		return "", fmt.Errorf("This number is not on WhatsApp")
	}
	return address, nil
}
