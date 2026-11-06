package api

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/wa"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *Server) taskRoutes(add func(string, string, route)) {
	svc := s.MCP.TaskService()
	add("GET /v1/tasks/{id}/cover/thumbnail", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		w.Header().Set("Cache-Control", "no-store")
		return svc.CoverPreview(nil, r.PathValue("id"))
	})
	add("POST /v1/tasks/{id}/notes", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			ID     string `json:"id"`
			Text   string `json:"text"`
			Remove bool   `json:"remove"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.OwnerNote(nil, r.PathValue("id"), b.ID, b.Text, b.Remove)
	})
	add("PATCH /v1/tasks/{id}/items/{item}/metadata", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			OCR         string `json:"ocr"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		task, err := svc.Get(nil, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		for _, it := range task.Items {
			if it.ID == r.PathValue("item") {
				return svc.NameAsset(task.ID, it.ID, b.Name, b.Description, b.OCR, b.Width, b.Height)
			}
		}
		return nil, model.ErrNotFound
	})
	add("GET /v1/tasks/{id}/items/{item}/preview", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		task, err := svc.Get(nil, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		for _, it := range task.Items {
			if it.ID != r.PathValue("item") || it.Kind != "file" {
				continue
			}
			result := map[string]any{"path": "", "thumbnail": []byte(nil)}
			if ref := it.MessageRef; ref != nil {
				// Get rechecks hidden/deleted source messages before returning items.
				var thumb []byte
				if err := s.Hub.Messages.DB.QueryRow("SELECT preview_thumbnail FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", ref.ChatJID, ref.MessageID).Scan(&thumb); err != nil {
					return nil, model.ErrNotFound
				}
				if len(thumb) <= 256<<10 {
					result["thumbnail"] = thumb
				}
				path := filepath.Join(s.Hub.Home, "media", dbutil.Digest(ref.ChatJID+"\x00"+ref.MessageID))
				if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
					result["path"] = path
				}
			} else {
				var a model.Attachment
				if s.Hub.Repo.Get("attachments", it.AttachmentID, &a) == nil && filepath.Base(a.ID) == a.ID {
					path := filepath.Join(s.Hub.Home, "outbox", a.ID)
					if _, err := os.Stat(path); err == nil {
						result["path"] = path
					}
				}
			}
			return result, nil
		}
		return nil, model.ErrNotFound
	})

	add("POST /v1/tasks/{id}/reopen", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Reopen(r.PathValue("id"))
	})
	add("POST /v1/tasks/{id}/items/{item}/download", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		task, err := svc.Get(nil, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		for _, item := range task.Items {
			if item.ID != r.PathValue("item") || item.Kind != "file" {
				continue
			}
			if ref := item.MessageRef; ref != nil {
				if s.Hub.HiddenChat(ref.ChatJID) || s.MCP.Downloader == nil {
					return nil, model.ErrNotFound
				}
				messages, err := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
				if err != nil || len(messages) == 0 || messages[0].DeletedAt != nil {
					return nil, model.ErrNotFound
				}
				path, err := s.MCP.Downloader.Download(r.Context(), ref.ChatJID, ref.MessageID)
				return map[string]string{"path": path}, err
			}
			var attachment model.Attachment
			if s.Hub.Repo.Get("attachments", item.AttachmentID, &attachment) != nil || filepath.Base(attachment.ID) != attachment.ID {
				return nil, model.ErrNotFound
			}
			path := filepath.Join(s.Hub.Home, "outbox", attachment.ID)
			if _, err := os.Stat(path); err != nil {
				return nil, model.ErrNotFound
			}
			return map[string]string{"path": path}, nil
		}
		return nil, model.ErrNotFound
	})

	add("POST /v1/tasks/{id}/keep", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Keep(nil, r.PathValue("id"))
	})
	add("POST /v1/tasks/{id}/dismiss", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Dismiss(nil, r.PathValue("id"), "")
	})
	add("POST /v1/tasks/{id}/undo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Undo(r.PathValue("id"))
	})
	add("POST /v1/settings/accessory-cue/test", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		ctx := s.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ok := svc.Accessory != nil && svc.Accessory.Test(ctx, s.Hub.Settings().AccessoryCue)
		return map[string]bool{"ok": ok, "running": ok}, nil
	})
	add("GET /v1/tasks/feed", "status", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var since time.Time
		var err error
		if raw := r.URL.Query().Get("since"); raw != "" {
			since, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				return nil, model.ErrInvalid
			}
		}
		xs, err := svc.Feed(since)
		return map[string]any{"tasks": xs}, err
	})
	add("GET /v1/tasks", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		list := svc.ListForPerson
		if sort := r.URL.Query().Get("sort"); sort == "recent" {
			list = svc.ListRecent
		} else if sort != "" {
			return nil, model.ErrInvalid
		}
		xs, err := list(nil, r.URL.Query().Get("status"), r.URL.Query().Get("person"), integer(r, "limit", 100))
		return map[string]any{"tasks": xs}, err
	})
	add("GET /v1/tasks/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Get(nil, r.PathValue("id"))
	})
	add("POST /v1/tasks", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Create
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Create(nil, b)
	})
	add("PATCH /v1/tasks/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b map[string]any
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Update(nil, r.PathValue("id"), b)
	})
	add("POST /v1/tasks/{id}/items", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Item
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Attach(nil, r.PathValue("id"), b)
	})
	add("DELETE /v1/tasks/{id}/items/{item}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.RemoveItem(nil, r.PathValue("id"), r.PathValue("item"))
	})
	add("POST /v1/tasks/{id}/checklist", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Step
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Checklist(nil, r.PathValue("id"), "add", []tasks.Step{b})
	})
	add("PATCH /v1/tasks/{id}/checklist/{step}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Text  *string `json:"text"`
			Done  *bool   `json:"done"`
			Order *int    `json:"order"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		if b.Text == nil && b.Done == nil && b.Order == nil {
			return nil, model.ErrInvalid
		}
		return svc.UpdateStep(r.PathValue("id"), r.PathValue("step"), b.Text, b.Done, b.Order)
	})
	add("DELETE /v1/tasks/{id}/checklist/{step}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Checklist(nil, r.PathValue("id"), "remove", []tasks.Step{{ID: r.PathValue("step")}})
	})
	add("POST /v1/tasks/{id}/nudge/skip", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.SkipNudge(nil, r.PathValue("id"))
	})
}
