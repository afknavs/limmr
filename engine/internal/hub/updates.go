package hub

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"limmr/internal/model"
	"time"
)

// UpdateCursor is encrypted and authenticated with a per-install key. It survives
// restarts and cannot be replayed by another agent or widened by editing offsets.
type UpdateCursor struct {
	Agent string
	Seq   int64
	Since time.Time
}

func (s *Service) updateCipher() (cipher.AEAD, error) {
	var key string
	if err := s.Repo.Get("updates", "cursor_key", &key); err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s *Service) ReadUpdateCursor(agent, raw string) (UpdateCursor, error) {
	c := UpdateCursor{Agent: agent, Since: s.Now().UTC().Add(-24 * time.Hour)}
	if raw == "" {
		return c, nil
	}
	if len(raw) > 2048 {
		return c, model.ErrInvalid
	}
	a, err := s.updateCipher()
	if err != nil {
		return c, err
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) < a.NonceSize() {
		return c, model.ErrInvalid
	}
	plain, err := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("limmr-updates-v1"))
	if err != nil || json.Unmarshal(plain, &c) != nil || c.Agent != agent || c.Seq < 0 {
		return UpdateCursor{}, model.ErrInvalid
	}
	return c, nil
}
func (s *Service) WriteUpdateCursor(c UpdateCursor) (string, error) {
	a, err := s.updateCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, b, []byte("limmr-updates-v1"))), nil
}
