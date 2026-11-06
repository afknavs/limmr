package hub

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"limmr/internal/attachments"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"time"
)

// The limit applies to the encoded chunk, keeping every tool call below 1 MiB.
const MaxChunkBase64 = 1 << 20
const MaxChunkDecoded = MaxChunkBase64 / 4 * 3
const MaxUploadReserved = 256 << 20

type Upload struct {
	ID           string         `json:"upload_id"`
	OwnerID      string         `json:"owner_id"`
	Name         string         `json:"name"`
	Total        int64          `json:"total_size"`
	Count        int            `json:"chunk_count"`
	Received     map[int]string `json:"received"`
	Bytes        int64          `json:"uploaded_bytes"`
	Missing      []int          `json:"missing_chunks"`
	ExpiresAt    time.Time      `json:"expires_at"`
	AttachmentID string         `json:"attachment_id,omitempty"`
	Status       string         `json:"status"`
}

func (s *Service) ChunkUpload(agent, id, name string, count int, total int64, index *int, encoded *string) (Upload, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var u Upload
	if id == "" {
		if count < 1 || count > 256 || total < 1 || total > attachments.MaxDocument || total > int64(count)*MaxChunkDecoded || name == "" || index == nil || encoded == nil {
			return u, fmt.Errorf("Start with filename, total_size (1..100 MiB), chunk_count (1..256), chunk_index and content_base64 (at most 1 MiB encoded).")
		}
		all, e := List[Upload](s.Repo, "uploads")
		if e != nil {
			return u, e
		}
		active := 0
		reserved := total
		for _, x := range all {
			if !s.Now().Before(x.ExpiresAt) {
				_ = os.RemoveAll(filepath.Join(s.Home, "uploads", x.ID))
				_ = s.Repo.Delete("uploads", x.ID)
				continue
			}
			if x.AttachmentID == "" {
				reserved += x.Total
				if x.OwnerID == agent {
					active++
				}
			}
		}
		if active >= 4 || reserved > MaxUploadReserved {
			return u, fmt.Errorf("Upload capacity reached (4 active per AI, 256 MiB total). Finish an upload or wait one hour for expiry.")
		}
		u = Upload{ID: dbutil.ID(), OwnerID: agent, Name: attachments.Name(name), Total: total, Count: count, Received: map[int]string{}, ExpiresAt: s.Now().Add(time.Hour), Status: "uploading"}
	} else {
		if e := s.Repo.Get("uploads", id, &u); e != nil || u.OwnerID != agent {
			return u, model.ErrNotFound
		}
		if !s.Now().Before(u.ExpiresAt) {
			_ = os.RemoveAll(filepath.Join(s.Home, "uploads", u.ID))
			return Upload{}, fmt.Errorf("Upload expired after one hour. Start a new upload without upload_id.")
		}
		if name != "" && attachments.Name(name) != u.Name || count != 0 && count != u.Count || total != 0 && total != u.Total {
			return u, fmt.Errorf("Upload metadata changed. Resume with the original metadata or start a new upload.")
		}
	}
	if index != nil || encoded != nil {
		if index == nil || encoded == nil || *index < 0 || *index >= u.Count || len(*encoded) > MaxChunkBase64 {
			return u, fmt.Errorf("Supply chunk_index within chunk_count and content_base64 of at most 1 MiB encoded. Split into smaller chunks.")
		}
		b, e := base64.StdEncoding.Strict().DecodeString(*encoded)
		if e != nil || len(b) == 0 {
			return u, fmt.Errorf("Invalid or empty base64 chunk. Encode the chunk again.")
		}
		hash := dbutil.Digest(string(b))
		old, exists := u.Received[*index]
		if exists && old != hash {
			return u, fmt.Errorf("This chunk index already contains different bytes. Start a new upload.")
		}
		if !exists {
			if u.Bytes+int64(len(b)) > u.Total {
				return u, fmt.Errorf("Chunks exceed total_size. Start again with the correct size (documents 100 MiB; media 16 MiB).")
			}
			dir := filepath.Join(s.Home, "uploads", u.ID)
			if e = os.MkdirAll(dir, 0700); e != nil {
				return u, e
			}
			path := filepath.Join(dir, fmt.Sprint(*index))
			if e = os.WriteFile(path, b, 0600); e != nil {
				return u, e
			}
			u.Received[*index] = hash
			u.Bytes += int64(len(b))
		}
	}
	u.Missing = []int{}
	for i := 0; i < u.Count; i++ {
		if _, ok := u.Received[i]; !ok {
			u.Missing = append(u.Missing, i)
		}
	}
	if len(u.Missing) == 0 && u.AttachmentID == "" {
		if u.Bytes != u.Total {
			return u, fmt.Errorf("Completed chunks do not match total_size. Start a new upload with the correct size.")
		}
		var data bytes.Buffer
		for i := 0; i < u.Count; i++ {
			b, e := os.ReadFile(filepath.Join(s.Home, "uploads", u.ID, fmt.Sprint(i)))
			if e != nil || dbutil.Digest(string(b)) != u.Received[i] {
				return u, fmt.Errorf("Stored chunk is unavailable. Start a new upload.")
			}
			data.Write(b)
		}
		b := data.Bytes()
		if e := attachments.CheckSize(int64(len(b)), attachments.MIME(b, u.Name)); e != nil {
			return u, e
		}
		at := model.Attachment{ID: u.ID, Name: u.Name, Size: u.Total, MIME: attachments.MIME(b, u.Name), SHA256: dbutil.Digest(string(b)), CreatedAt: s.Now()}
		if e := os.MkdirAll(filepath.Join(s.Home, "outbox"), 0700); e != nil {
			return u, e
		}
		if e := os.WriteFile(filepath.Join(s.Home, "outbox", at.ID), b, 0600); e != nil {
			return u, e
		}
		u.AttachmentID = at.ID
		u.Status = "uploaded"
		if e := s.Repo.Apply(Mutation{Bucket: "attachments", ID: at.ID, Value: at}, Mutation{Bucket: "attachment_owners", ID: at.ID, Value: agent}, Mutation{Bucket: "uploads", ID: u.ID, Value: u}); e != nil {
			return u, e
		}
		_ = os.RemoveAll(filepath.Join(s.Home, "uploads", u.ID))
	}
	return u, s.Repo.Put("uploads", u.ID, u)
}
