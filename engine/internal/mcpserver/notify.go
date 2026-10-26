package mcpserver

import (
	"context"
	"fmt"
	"limmr/internal/attachments"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"time"
)

func fileSchema(t *Tool) {
	props := t.InputSchema["properties"].(map[string]any)
	props["file"] = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "object", "properties": map[string]any{"download_url": map[string]any{"type": "string"}, "file_id": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "mime_type": map[string]any{"type": "string"}}, "required": []string{"download_url"}, "additionalProperties": false}}}
	t.Meta = map[string]any{"openai/fileParams": []string{"file"}}
}
func hasSource(a Args) bool {
	for _, k := range []string{"attachment_id", "content_text", "content_base64", "url", "file"} {
		if _, ok := a[k]; ok {
			return true
		}
	}
	return false
}
func (s *Service) attachment(ctx context.Context, a model.Agent, args Args) (model.Attachment, error) {
	phase(ctx, "bridge")
	src := attachments.Source{AttachmentID: args.str("attachment_id"), Filename: args.str("filename"), URL: args.str("url"), File: args["file"]}
	if v, ok := args["content_text"].(string); ok {
		src.Text = &v
	}
	if v, ok := args["content_base64"].(string); ok {
		src.Base64 = &v
	}
	f := s.Fetcher
	if f == nil {
		f = &attachments.Fetcher{}
	}
	b, name, e := f.Read(ctx, src)
	if e != nil {
		return model.Attachment{}, e
	}
	if src.AttachmentID != "" {
		var at model.Attachment
		e = s.Hub.Repo.Get("attachments", src.AttachmentID, &at)
		if e != nil {
			return at, e
		} // Reuse only this AI's uploads or owner-uploaded attachments.
		var owner string
		if err := s.Hub.Repo.Get("attachment_owners", at.ID, &owner); err == nil {
			if owner != a.ID {
				return model.Attachment{}, model.ErrNotFound
			}
		} else if err != model.ErrNotFound {
			return model.Attachment{}, err
		}
		info, err := os.Stat(filepath.Join(s.Hub.Home, "outbox", at.ID))
		if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(s.Hub.Now().Add(-7*24*time.Hour)) {
			return at, model.ErrNotFound
		}
		return at, nil
	}
	at, e := s.Hub.Attachment(b, name)
	if e == nil {
		e = s.Hub.Repo.Put("attachment_owners", at.ID, a.ID)
	}
	return at, e
}
func permissionError(a model.Agent, name string) error {
	label := "This capability"
	switch name {
	case "send_message", "send_file", "send_reaction", "notify_me", "upload_attachment", "draft_message", "send_draft", "start_handoff":
		label = "Sending"
	case "download_media":
		label = "Media downloads"
	case "save_summary", "update_brief":
		label = "Saving summaries"
	case "search_messages":
		label = "Search"
	case "update_person", "update_owner_profile", "add_fact", "add_relation", "propose_circle", "add_to_circle":
		label = "People editing"
	default:
		label = "Reading"
	}
	return fmt.Errorf("%s is off for %s. %s can allow it in Limmr › Integrations › %s.", label, a.Name, "The owner", a.Name)
}
func (s *Service) connection(a model.Agent) map[string]any {
	linked, connected := false, false
	var at *time.Time
	if s.Link != nil {
		linked, connected, at = s.Link.Connection()
	}
	health := "WhatsApp is not linked"
	if linked {
		health = "WhatsApp is reconnecting"
	}
	if !connected && s.Link != nil {
		if state := s.Link.State(); state.State == "error" && state.Reason != "" {
			health = state.Reason
		}
	}
	if connected {
		health = "Connected"
		if at != nil {
			age := s.Hub.Now().Sub(*at)
			if age < time.Minute {
				health += " · last message just now"
			} else {
				health += fmt.Sprintf(" · last message %d min ago", int(age.Minutes()))
			}
		}
	}
	return map[string]any{"agent": map[string]any{"id": a.ID, "name": a.Name}, "whatsapp": map[string]any{"linked": linked, "connected": connected, "last_event_at": at}, "can_send": connected && a.Enabled && a.Permissions.RequestSend && !a.Permissions.Access.Ended(s.Hub.Now()), "health": health}
}
