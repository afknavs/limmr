package tasks

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	if t.CoverRemoved {
		return nil
	}
	picked := t.CoverItemID
	if picked == "" && t.Cover != nil {
		picked = t.Cover.ID
	}
	fallback := func() *CoverCandidate {
		return &CoverCandidate{ItemID: defaultCoverID(t.ID), Title: "Task cover", ThumbnailURL: "/v1/tasks/" + url.PathEscape(t.ID) + "/cover/thumbnail?default=" + defaultCoverID(t.ID)}
	}
	if picked == "" || strings.HasPrefix(picked, "default:") {
		return fallback()
	}
	var raw string
	var it Item
	err := s.Hub.Repo.DB.QueryRow(`SELECT json_object('id',json_extract(value,'$.id'),'title',json_extract(value,'$.title'),'media_type',json_extract(value,'$.media_type'),'attachment_id',json_extract(value,'$.attachment_id'),'message_ref',json_extract(value,'$.message_ref')) FROM tasks,json_each(tasks.data,'$.items') WHERE tasks.id=? AND json_extract(value,'$.id')=?`, t.ID, picked).Scan(&raw)
	if err != nil || json.Unmarshal([]byte(raw), &it) != nil || it.MediaType != "image" {
		return fallback()
	}
	c := &CoverCandidate{ItemID: it.ID, Title: it.Title, attachmentID: it.AttachmentID}
	if ref := it.MessageRef; ref != nil {
		if s.Hub.HiddenChat(ref.ChatJID) || !s.Hub.CanChat(a, ref.ChatJID) {
			return fallback()
		}
		var exists int
		if s.Hub.Messages.DB.QueryRow("SELECT 1 FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", ref.ChatJID, ref.MessageID).Scan(&exists) != nil {
			return fallback()
		}
		c.ChatJID = ref.ChatJID
		c.MediaID = ref.MessageID
	}
	var revision int64
	_ = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&revision)
	c.ThumbnailURL = "/v1/tasks/" + url.PathEscape(t.ID) + "/cover/thumbnail?revision=" + dbutil.Digest(c.ItemID+c.ChatJID+c.MediaID+strconv.FormatInt(revision, 10))
	return c
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
	if strings.HasPrefix(c.ItemID, "default:") {
		path, err := s.defaultCover(t.ID)
		return map[string]any{"path": path, "thumbnail": []byte(nil)}, err
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
