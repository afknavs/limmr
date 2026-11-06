package tasks

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// This projection deliberately contains no captions, OCR, summaries or bodies.
// The URL is an authenticated, local preview read, never a download/send action.
type CoverCandidate struct {
	ItemID       string `json:"item_id,omitempty"`
	MediaID      string `json:"media_id,omitempty"`
	ChatJID      string `json:"chat_jid,omitempty"`
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnail_url"`
	LinkURL      string `json:"link_url,omitempty"`
	attachmentID string
}

func (s *Service) coverCandidate(a *model.Agent, t Task) *CoverCandidate {
	return s.selectCoverCandidate(a, t, true)
}

// drawable=false identifies the single best source for the background fetcher.
func (s *Service) selectCoverCandidate(a *model.Agent, t Task, drawable bool) *CoverCandidate {
	if t.CoverRemoved {
		return nil
	}
	// Read only safe item metadata in array order. Do not decode task workspaces.
	rows, err := s.Hub.Repo.DB.Query(`SELECT json_object('id',json_extract(i.value,'$.id'),'kind',json_extract(i.value,'$.kind'),'title',json_extract(i.value,'$.title'),'added_at',json_extract(i.value,'$.added_at'),'media_type',json_extract(i.value,'$.media_type'),'message_ref',json_extract(i.value,'$.message_ref'),'attachment_id',json_extract(i.value,'$.attachment_id'),'url',json_extract(i.value,'$.url')) FROM tasks t,json_each(t.data,'$.items') i WHERE t.id=? AND (json_extract(i.value,'$.media_type')='image' OR json_extract(i.value,'$.kind')='link' OR json_extract(i.value,'$.id')=?) ORDER BY CAST(i.key AS INTEGER)`, t.ID, t.CoverItemID)
	if err != nil {
		return nil
	}
	items := []Item{}
	for rows.Next() {
		var raw string
		var it Item
		if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &it) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	finish := func(c *CoverCandidate) *CoverCandidate {
		var revision int64
		_ = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&revision)
		c.ThumbnailURL = "/v1/tasks/" + url.PathEscape(t.ID) + "/cover/thumbnail?revision=" + dbutil.Digest(c.ItemID+c.ChatJID+c.MediaID+t.UpdatedAt.Format(time.RFC3339Nano)+strconv.FormatInt(revision, 10))
		return c
	}
	fromItem := func(it Item) *CoverCandidate {
		c := &CoverCandidate{ItemID: it.ID, Title: it.Title, attachmentID: it.AttachmentID}
		if ref := it.MessageRef; ref != nil {
			if s.Hub.HiddenChat(ref.ChatJID) || !s.Hub.CanChat(a, ref.ChatJID) {
				return nil
			}
			var exists int
			if s.Hub.Messages.DB.QueryRow("SELECT 1 FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", ref.ChatJID, ref.MessageID).Scan(&exists) != nil {
				return nil
			}
			c.ChatJID = ref.ChatJID
			c.MediaID = ref.MessageID
		}
		if it.Kind == "link" {
			c.LinkURL = normalizeURL(it.URL)
		}
		return finish(c)
	}
	// Owner choice wins even when the original is not on disk yet.
	for _, it := range items {
		if it.ID == t.CoverItemID && t.CoverItemID != "" {
			if c := fromItem(it); c != nil {
				return c
			}
		}
	}
	var workspace *CoverCandidate
	var workspaceAt time.Time
	workspaceFile := false
	for _, it := range items {
		if it.MediaType == "image" {
			if c := fromItem(it); c != nil {
				hasFile := s.coverHasFile(c)
				if drawable && !hasFile && !s.coverHasPixels(c) {
					continue
				}
				if workspace == nil || drawable && hasFile && !workspaceFile || (!drawable || hasFile == workspaceFile) && it.AddedAt.After(workspaceAt) {
					workspace, workspaceAt, workspaceFile = c, it.AddedAt, hasFile
				}
			}
		}
	}
	if workspace != nil {
		return workspace
	}

	// Each linked chat uses its indexed timeline. Compare only metadata, never text.
	var newest time.Time
	var media *CoverCandidate
	for _, chat := range t.Chats {
		if s.Hub.HiddenChat(chat.ID) || !s.Hub.CanChat(a, chat.ID) {
			continue
		}
		rows, err := s.Hub.Messages.DB.Query("SELECT id,COALESCE(filename,''),timestamp,length(preview_thumbnail) FROM messages WHERE chat_jid=? AND media_type='image' AND deleted_at IS NULL ORDER BY julianday(timestamp) DESC,id DESC LIMIT 50", chat.ID)
		if err != nil {
			continue
		}
		var chatMedia *CoverCandidate
		var chatAt time.Time
		for rows.Next() {
			var id, title string
			var at time.Time
			var thumbnailSize sql.NullInt64
			if rows.Scan(&id, &title, &at, &thumbnailSize) != nil {
				continue
			}
			c := &CoverCandidate{MediaID: id, ChatJID: chat.ID, Title: title}
			hasFile := s.coverHasFile(c)
			if drawable && thumbnailSize.Int64 == 0 && !hasFile {
				continue
			}
			if chatMedia == nil || hasFile {
				chatAt, chatMedia = at, c
			}
			if !drawable || hasFile {
				break
			}
		}
		rows.Close()
		if chatMedia != nil && (media == nil || chatAt.After(newest) || chatAt.Equal(newest) && chatMedia.MediaID > media.MediaID) {
			newest, media = chatAt, chatMedia
		}
	}
	if media != nil {
		return finish(media)
	}
	for _, it := range items {
		if it.Kind == "link" {
			if c := fromItem(it); c != nil && c.LinkURL != "" {
				return c
			}
		}
	}
	return nil
}

// CoverPreview rechecks current task/source visibility even if the app has pixels.
// It never loads message text, task history or a workspace and never downloads.
func (s *Service) CoverPreview(a *model.Agent, id string) (map[string]any, error) {
	var t Task
	var raw string
	if err := s.Hub.Repo.DB.QueryRow("SELECT data FROM task_cards WHERE id=?", id).Scan(&raw); err != nil {
		return nil, model.ErrNotFound
	}
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, err
	}
	if err := s.authorize(a, t, false); err != nil {
		return nil, err
	}
	c := s.coverCandidate(a, t)
	if c == nil {
		return nil, model.ErrNotFound
	}
	path, thumb := s.coverPixels(c)
	cached, err := s.coverThumbnail(path, thumb)
	if err != nil {
		return map[string]any{"path": "", "thumbnail": []byte(nil)}, nil
	}
	return map[string]any{"path": cached, "thumbnail": []byte(nil)}, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}
func (s *Service) coverMediaPath(c *CoverCandidate) string {
	if c.MediaID != "" {
		return filepath.Join(s.Hub.Home, "media", dbutil.Digest(c.ChatJID+"\x00"+c.MediaID))
	}
	if c.attachmentID != "" {
		var attachment model.Attachment
		if s.Hub.Repo.Get("attachments", c.attachmentID, &attachment) == nil && attachment.ID != "" && filepath.Base(attachment.ID) == attachment.ID {
			return filepath.Join(s.Hub.Home, "outbox", attachment.ID)
		}
	}
	return ""
}
func (s *Service) coverHasFile(c *CoverCandidate) bool {
	return regularFile(s.coverMediaPath(c))
}
func (s *Service) coverHasPixels(c *CoverCandidate) bool {
	if s.coverHasFile(c) {
		return true
	}
	var size int
	return c.MediaID != "" && s.Hub.Messages.DB.QueryRow("SELECT COALESCE(length(preview_thumbnail),0) FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", c.ChatJID, c.MediaID).Scan(&size) == nil && size > 0
}
func (s *Service) coverPixels(c *CoverCandidate) (string, []byte) {
	path := s.coverMediaPath(c)
	var thumb []byte
	if c.MediaID != "" {
		_ = s.Hub.Messages.DB.QueryRow("SELECT preview_thumbnail FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", c.ChatJID, c.MediaID).Scan(&thumb)
	}
	return path, thumb
}
