package remote

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mediaGrant struct {
	Agent, Path, Chat, Message, PublicURL string
	Expires                               time.Time
}

func (s *Service) mediaSignature(nonce string) string {
	m := hmac.New(sha256.New, []byte(s.mediaKey))
	m.Write([]byte(nonce))
	return hex.EncodeToString(m.Sum(nil))
}
func (s *Service) MediaURL(a model.Agent, path, chat, message string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled || !a.Permissions.DownloadMedia || !s.Hub.CanChat(&a, chat) {
		return "", model.ErrForbidden
	}
	root, e := filepath.EvalSymlinks(filepath.Join(s.Hub.Home, "media"))
	if e != nil {
		return "", model.ErrNotFound
	}
	clean, e := filepath.EvalSymlinks(path)
	if e != nil || !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return "", model.ErrForbidden
	}
	for k, v := range s.media {
		if !s.Now().Before(v.Expires) {
			delete(s.media, k)
		}
	}
	if len(s.media) >= 256 {
		return "", model.ErrConflict
	}
	nonce := dbutil.ID() + dbutil.ID()
	token := nonce + "." + s.mediaSignature(nonce)
	s.media[dbutil.Digest(token)] = mediaGrant{a.ID, clean, chat, message, s.config.PublicURL, s.Now().Add(15 * time.Minute)}
	return s.config.PublicURL + "/media/" + token, nil
}
func (s *Service) serveMedia(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	nonce, sig, ok := strings.Cut(token, ".")
	s.mu.Lock()
	g, found := s.media[dbutil.Digest(token)]
	valid := s.config.Enabled && ok && found && hmac.Equal([]byte(sig), []byte(s.mediaSignature(nonce))) && s.Now().Before(g.Expires) && g.PublicURL == s.config.PublicURL
	// Consume under the lock before serving. Concurrent requests cannot both use it.
	if valid {
		delete(s.media, dbutil.Digest(token))
	}
	s.mu.Unlock()
	if !valid {
		http.NotFound(w, r)
		return
	}
	a, e := s.Hub.ActiveAgent(g.Agent)
	if e != nil || !a.Permissions.DownloadMedia || s.Hub.CheckAccess(a) != nil || !s.Hub.CanChat(&a, g.Chat) {
		http.NotFound(w, r)
		return
	}
	clean, e := filepath.EvalSymlinks(g.Path)
	if e != nil || clean != g.Path {
		http.NotFound(w, r)
		return
	}
	f, e := os.Open(clean)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment")
	http.ServeContent(w, r, "attachment", info.ModTime(), f)
}
