package people

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/jpeg"
	"io"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AvatarLookup is read-only. It must never send presence or read receipts.
type AvatarLookup interface {
	FetchAvatar(context.Context, string) ([]byte, error)
}
type AvatarCache struct {
	Hub    *hub.Service
	Lookup AvatarLookup
	mu     sync.Mutex
}
type avatarEntry struct {
	AttemptedAt time.Time `json:"attempted_at"`
	Available   bool      `json:"available"`
}

const MaxAvatarBytes = 4 << 20

func ValidAvatar(data []byte) bool {
	if len(data) > MaxAvatarBytes {
		return false
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	return err == nil && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 4096 && cfg.Height <= 4096
}

func (s *AvatarCache) Get(ctx context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p model.Person
	jid := ""
	if strings.HasSuffix(id, "@g.us") {
		var g model.Group
		if err := s.Hub.Repo.Get("groups", id, &g); err != nil {
			return nil, err
		}
		jid = id
	} else {
		if err := s.Hub.Repo.Get("people", id, &p); err != nil {
			return nil, err
		}
		for _, ref := range []*model.PhotoReference{p.Photo, p.ContactsPhoto} {
			if ref != nil {
				if data, e := readProfilePhoto(s.Hub.Home, ref.SHA256); e == nil {
					return data, nil
				}
			}
		}
	}
	if s.Lookup == nil {
		return nil, model.ErrNotFound
	} // demo never performs a lookup
	for _, j := range p.JIDs {
		parts := strings.Split(j, "@")
		if len(parts) == 2 && parts[0] != "" && (parts[1] == "s.whatsapp.net" || parts[1] == "lid") {
			jid = j
			break
		}
	}
	if jid == "" {
		return nil, model.ErrNotFound
	}
	var entry avatarEntry
	err := s.Hub.Repo.Get("avatar_cache", id, &entry)
	if err != nil && err != model.ErrNotFound {
		return nil, err
	}
	dir := filepath.Join(s.Hub.Home, "avatars")
	path := filepath.Join(dir, dbutil.Digest(id)+".jpg")
	cached := func() ([]byte, error) {
		if entry.Available {
			data, e := os.ReadFile(path)
			if e == nil && ValidAvatar(data) {
				return data, nil
			}
		}
		return nil, model.ErrNotFound
	}
	now := s.Hub.Now().UTC()
	if !entry.AttemptedAt.IsZero() && now.Sub(entry.AttemptedAt) < 7*24*time.Hour {
		return cached()
	}
	var last time.Time
	err = s.Hub.Repo.Get("avatar_meta", "last_lookup", &last)
	if err != nil && err != model.ErrNotFound {
		return nil, err
	}
	if !last.IsZero() && now.Sub(last) < 2*time.Second {
		return cached()
	}
	// Persist attempted lookups, including misses/failures, before network access.
	entry.AttemptedAt = now
	if err = s.Hub.Repo.Apply(hub.Mutation{Bucket: "avatar_cache", ID: id, Value: entry}, hub.Mutation{Bucket: "avatar_meta", ID: "last_lookup", Value: now}); err != nil {
		return nil, err
	}
	data, err := s.Lookup.FetchAvatar(ctx, jid)
	if err != nil || !ValidAvatar(data) {
		return cached()
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".avatar-")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, path); err != nil {
		return nil, err
	}
	entry.Available = true
	if err = s.Hub.Repo.Put("avatar_cache", id, entry); err != nil {
		return nil, err
	}
	return data, nil
}

// Local imports use .jpg; the sync transport stores the same normalized bytes at
// an extensionless digest. Neither missing nor malformed data triggers live I/O.
func readProfilePhoto(home, hash string) ([]byte, error) {
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(hash) != hash {
		return nil, model.ErrInvalid
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, ext := range []string{".jpg", ""} {
		name := "avatars/custom/" + hash + ext
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > MaxAvatarBytes {
			return nil, model.ErrInvalid
		}
		file, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, MaxAvatarBytes+1))
		file.Close()
		sum := sha256.Sum256(data)
		if err == nil && hex.EncodeToString(sum[:]) == hash && ValidAvatar(data) {
			return data, nil
		}
	}
	return nil, model.ErrNotFound
}
