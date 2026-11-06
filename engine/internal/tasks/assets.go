package tasks

import (
	"context"
	"database/sql"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func assetName(it Item) string {
	text := strings.Join(strings.Fields(it.Caption), " ")
	if text != "" {
		return short(text, 88)
	}
	name := strings.TrimSuffix(it.Title, "."+lastExtension(it.Title))
	name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
	if it.MediaType == "image" && (len(name) < 5 || strings.HasPrefix(strings.ToLower(name), "img") || strings.HasPrefix(strings.ToLower(name), "image")) {
		return "Photo from " + it.Chat
	}
	if name != "" {
		return short(name, 88)
	}
	return "Shared " + it.MediaType
}
func lastExtension(s string) string {
	p := strings.LastIndex(s, ".")
	if p < 0 {
		return ""
	}
	return s[p+1:]
}

// The largest task card is about 480×320 pixels; use at least twice that.
const coverWidth, coverHeight = 960, 640

func coverWords(text string) map[string]bool {
	out := map[string]bool{}
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "task": true, "make": true, "this": true, "that": true, "your": true}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len([]rune(w)) >= 3 && !stop[w] {
			out[w] = true
		}
	}
	return out
}
func coverScore(t Task, it Item) int {
	if it.MediaType != "image" && it.MediaType != "document" {
		return -1
	}
	text := strings.ToLower(it.Caption + " " + it.AIName + " " + it.OCRText + " " + it.Description + " " + it.Title)
	for _, skip := range []string{"screenshot", "screen shot", "chat capture", "whatsapp chat", "sticker", "reaction", "meme", "when you", "when your", "me when", "nobody:", "no one:", "expectation vs", "bro thinks", "pov:", "lol", "lmao"} {
		if strings.Contains(text, skip) {
			return -1
		}
	}
	explicit := it.Source == "owner" || it.Source == "ai"
	if !explicit {
		if it.MessageRef == nil {
			return -1
		}
		linked := false
		for _, ch := range t.Chats {
			linked = linked || ch.ID == it.MessageRef.ChatJID
		}
		if !linked {
			return -1
		}
		// A bare joke, emoji or unrelated media filename cannot supply relevance.
		caption := coverWords(it.Caption + " " + it.AIName + " " + it.OCRText)
		if len(caption) < 3 {
			return -1
		}
		matches := 0
		for w := range coverWords(t.Title + " " + t.Goal) {
			if caption[w] {
				matches++
			}
		}
		if matches == 0 {
			return -1
		}
	}
	score := 1
	if it.Height > it.Width {
		score += 2
	}
	if explicit {
		score += 10
	}
	return score
}
func (s *Service) fullCoverSize(it Item) (int, int) {
	path := ""
	if ref := it.MessageRef; ref != nil {
		path = filepath.Join(s.Hub.Home, "media", dbutil.Digest(ref.ChatJID+"\x00"+ref.MessageID))
	} else if it.AttachmentID != "" {
		if filepath.Base(it.AttachmentID) != it.AttachmentID {
			return 0, 0
		}
		path = filepath.Join(s.Hub.Home, "outbox", it.AttachmentID)
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
func (s *Service) chooseCover(t *Task) {
	t.Cover = nil
	if t.CoverRemoved {
		return
	}
	best := -1
	for _, it := range t.Items {
		picked := t.CoverItemID != "" && it.ID == t.CoverItemID
		if t.CoverItemID != "" && !picked {
			continue
		}
		score := coverScore(*t, it)
		if picked {
			score = 100
		}
		if score < 0 {
			continue
		}
		// Preview dimensions and thumbnails are never evidence of a usable original.
		width, height := s.fullCoverSize(it)
		if width < coverWidth || height < coverHeight {
			continue
		}
		if score > best || score == best && t.Cover != nil && it.AddedAt.After(t.Cover.AddedAt) {
			best = score
			copy := it
			copy.Width = width
			copy.Height = height
			copy.OCRText = ""
			copy.Caption = ""
			copy.Description = ""
			t.Cover = &copy
		}
	}
}

// Local analysis can name an asset but agents cannot forge owner notes or covers.
func (s *Service) NameAsset(id, itemID string, name, description, ocr string, width, height int) (Task, error) {
	if !length(name, 120) || !length(description, 800) || !length(ocr, 12000) || width < 0 || height < 0 {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		for i := range r.Items {
			it := &r.Items[i]
			if it.ID != itemID {
				continue
			}
			if strings.TrimSpace(name) != "" {
				it.AIName = strings.TrimSpace(name)
			}
			it.Description = description
			it.OCRText = ocr
			it.Width = width
			it.Height = height
			it.NamedOnDevice = true
			s.chooseCover(&r.Task)
			r.UpdatedAt = s.now()
			return nil
		}
		return model.ErrNotFound
	})
}
func (s *Service) OwnerNote(a *model.Agent, id, noteID, text string, remove bool) (Task, error) {
	if a != nil {
		return Task{}, model.ErrForbidden
	}
	text = strings.TrimSpace(text)
	if !remove && (text == "" || !length(text, 4000)) {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		now := s.now()
		found := false
		if noteID == "" {
			if remove || len(r.OwnerNotes) >= 200 {
				return model.ErrInvalid
			}
			noteID = "note_" + dbutil.ID()
			r.OwnerNotes = append(r.OwnerNotes, OwnerNote{ID: noteID, Text: text, CreatedAt: now, UpdatedAt: now})
			found = true
		} else {
			for i, n := range r.OwnerNotes {
				if n.ID == noteID {
					found = true
					if remove {
						r.OwnerNotes = append(r.OwnerNotes[:i], r.OwnerNotes[i+1:]...)
					} else {
						r.OwnerNotes[i].Text = text
						r.OwnerNotes[i].UpdatedAt = now
					}
					break
				}
			}
		}
		if !found {
			return model.ErrNotFound
		}
		sort.Slice(r.OwnerNotes, func(i, j int) bool { return r.OwnerNotes[i].CreatedAt.After(r.OwnerNotes[j].CreatedAt) })
		r.UpdatedAt = now
		return s.event(tx, *r, "note:"+dbutil.ID(), "task_owner_note", "Owner notes updated", map[string]string{"by": "you"})
	})
}

// Migration clears unsafe auto choices before API readiness. Reconsider once on
// startup using only local originals, preserving explicit owner choices.
func (s *Service) ReevaluateCovers(ctx context.Context) error {
	rs, err := s.records()
	if err != nil {
		return err
	}
	for _, old := range rs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !old.CoverReviewPending || old.Purged {
			continue
		}
		_, err = s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
			if !r.CoverReviewPending {
				return nil
			}
			s.chooseCover(&r.Task)
			r.CoverReviewPending = false
			return s.event(tx, *r, "cover_review_v17", "cover_reviewed", "Automatic cover reviewed", nil)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
