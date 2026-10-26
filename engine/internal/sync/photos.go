package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"limmr/internal/dbutil"
	"os"
)

func (s *Service) fetchPhotos(ctx context.Context, t Transport) error {
	hashes, err := s.profilePhotoHashes()
	if err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err = s.Blob(ctx, h); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		b, err := t.Blob(ctx, h)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		if len(b) > MaxBytes || hex.EncodeToString(sum[:]) != h || blobType(b) == "" {
			return ErrInvalid
		}
		root, err := os.OpenRoot(s.Home)
		if err != nil {
			return err
		}
		err = storePhoto(root, h, b)
		root.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Only owner-imported profile references are replicated. Message media and the
// WhatsApp avatar cache are deliberately excluded; retain inactive fallback bytes.
func (s *Service) profilePhotoHashes() ([]string, error) {
	rows, err := s.Repo.DB.Query(`SELECT json_extract(data,'$.photo.sha256') FROM objects WHERE bucket='people' AND json_type(data,'$.photo.sha256')='text'
UNION SELECT json_extract(data,'$.contacts_photo.sha256') FROM objects WHERE bucket='people' AND json_type(data,'$.contacts_photo.sha256')='text'`)
	if err != nil {
		return nil, err
	}
	hashes := []string{}
	for rows.Next() {
		var h string
		if err = rows.Scan(&h); err != nil {
			break
		}
		hashes = append(hashes, h)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return hashes, nil
}
func storePhoto(root *os.Root, hash string, data []byte) error {
	if err := root.MkdirAll("avatars/custom", 0700); err != nil {
		return err
	}
	// Root confines even racing symlink changes to the authorized engine home.
	name := "avatars/custom/.sync-" + dbutil.ID()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return root.Rename(name, "avatars/custom/"+hash)
}
