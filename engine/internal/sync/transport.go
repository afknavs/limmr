package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
)

// Transport is outbound-only; InProcess exercises the same protocol without sockets.
type Transport interface {
	Pull(context.Context, PullRequest) (PullResponse, error)
	Push(context.Context, PushRequest) (PushResponse, error)
	Blob(context.Context, string) ([]byte, error)
}
type InProcess struct{ Remote *Service }

func (t InProcess) Pull(c context.Context, q PullRequest) (PullResponse, error) {
	return t.Remote.Pull(c, q)
}
func (t InProcess) Push(c context.Context, q PushRequest) (PushResponse, error) {
	return t.Remote.Push(c, q)
}
func (t InProcess) Blob(c context.Context, hash string) ([]byte, error) {
	return t.Remote.Blob(c, hash)
}

// Exchange drains push and pull pages, persisting cursors only after acknowledged
// batches. Replaying after interruption is safe. Pair must already authorize one peer.
func (s *Service) Exchange(ctx context.Context, t Transport) (err error) {
	var p Peer
	if e := s.Repo.Get("settings", "sync_peer", &p); e != nil || p.EngineID == "" || p.Paused {
		return ErrInvalid
	}
	defer func() {
		if err != nil {
			p.LastError = "Your dot could not sync. Check the connection and try again."
			_ = s.Repo.Put("settings", "sync_peer", p)
		}
	}()
	// Pull first so a fresh takeover learns claims before any next automation tick.
	for {
		page, e := t.Pull(ctx, PullRequest{SinceSeq: p.LastPulledSeq, PeerEngineID: s.EngineID, SyncVersion: Version, Schema: Schema})
		if e != nil {
			return e
		}
		if page.EngineID != p.EngineID {
			return ErrInvalid
		}
		result, e := s.Push(ctx, PushRequest{Changes: page.Changes, FromEngineID: p.EngineID, SyncVersion: Version, Schema: Schema})
		if e != nil {
			return e
		}
		if e = s.fetchPhotos(ctx, t); e != nil {
			return e
		}
		p.LastPulledSeq = page.NextSeq
		p.Applied += result.Applied
		p.Stale += result.Stale
		if e = s.Repo.Put("settings", "sync_peer", p); e != nil {
			return e
		}
		if !page.More {
			break
		}
	}
	if uploader, ok := t.(interface {
		Upload(context.Context, *Service) error
	}); ok {
		if e := uploader.Upload(ctx, s); e != nil {
			return e
		}
	}
	for {
		page, e := s.Pull(ctx, PullRequest{SinceSeq: p.LastPushedSeq, PeerEngineID: p.EngineID, SyncVersion: Version, Schema: Schema})
		if e != nil {
			return e
		}
		_, e = t.Push(ctx, PushRequest{Changes: page.Changes, FromEngineID: s.EngineID, SyncVersion: Version, Schema: Schema})
		if e != nil {
			return e
		}
		p.Sent += len(page.Changes)
		p.LastPushedSeq = page.NextSeq
		if e = s.Repo.Put("settings", "sync_peer", p); e != nil {
			return e
		}
		if !page.More {
			break
		}
	}
	p.LastSyncAt = s.Now()
	p.LastError = ""
	return s.Repo.Put("settings", "sync_peer", p)
}

// Handler contains no authentication policy. Y7 must wrap it with the sync grant
// before mounting it on a listener. The push-specific bound does not alter any mux.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		limit := int64(64 << 10)
		if r.URL.Path == "/sync/push" {
			limit = MaxBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		defer r.Body.Close()
		decode := func(v any) error {
			d := json.NewDecoder(r.Body)
			if e := d.Decode(v); e != nil {
				return e
			}
			if d.Decode(new(any)) != io.EOF {
				return ErrInvalid
			}
			return nil
		}
		var out any
		var err error
		switch r.URL.Path {
		case "/sync/pull":
			var q PullRequest
			err = decode(&q)
			if err == nil {
				out, err = s.Pull(r.Context(), q)
			}
		case "/sync/push":
			var q PushRequest
			err = decode(&q)
			if err == nil {
				out, err = s.Push(r.Context(), q)
			}
		case "/sync/blob":
			var q struct {
				SHA256 string `json:"sha256"`
			}
			err = decode(&q)
			if err == nil {
				var b []byte
				b, err = s.Blob(r.Context(), q.SHA256)
				if err == nil {
					w.Header().Set("Content-Type", blobType(b))
					_, _ = w.Write(b)
					return
				}
			}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			// Reasons only; request bodies and credentials never reach the log.
			slog.Warn("sync request failed", "path", r.URL.Path, "error", err.Error())
			code := http.StatusBadRequest
			body := map[string]string{"error": "invalid_sync_request"}
			if errors.Is(err, ErrUpdateNeeded) {
				code = http.StatusConflict
				body = map[string]string{"error": "update_needed", "fix": "Update Limmr on the other device"}
			}
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		var peer Peer
		if s.Repo.Get("settings", "sync_peer", &peer) == nil && peer.EngineID != "" {
			peer.LastSyncAt = s.Now()
			peer.LastError = ""
			switch v := out.(type) {
			case PushResponse:
				peer.Applied += v.Applied
			case PullResponse:
				peer.Sent += len(v.Changes)
			}
			_ = s.Repo.Put("settings", "sync_peer", peer)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
func (s *Service) Blob(ctx context.Context, hash string) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	decoded, e := hex.DecodeString(hash)
	if e != nil || len(decoded) != sha256.Size || strings.ToLower(hash) != hash {
		return nil, ErrInvalid
	}
	root := filepath.Join(s.Home, "avatars", "custom")
	for _, dir := range []string{filepath.Join(s.Home, "avatars"), root} {
		fi, e := os.Lstat(dir)
		if e != nil {
			return nil, e
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
	}
	scoped, e := os.OpenRoot(root)
	if e != nil {
		return nil, e
	}
	defer scoped.Close()
	for _, ext := range []string{"", ".jpg", ".jpeg", ".png", ".heic"} {
		name := hash + ext
		fi, e := scoped.Lstat(name)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !fi.Mode().IsRegular() || fi.Size() > MaxBytes {
			return nil, ErrInvalid
		}
		f, e := scoped.Open(name)
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(f, MaxBytes+1))
		f.Close()
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(b)
		if len(b) > MaxBytes || hex.EncodeToString(sum[:]) != hash || blobType(b) == "" {
			return nil, ErrInvalid
		}
		return b, nil
	}
	return nil, os.ErrNotExist
}
func blobType(b []byte) string {
	mime := http.DetectContentType(b)
	if mime == "image/png" || mime == "image/jpeg" {
		return mime
	}
	if len(b) >= 12 && string(b[4:8]) == "ftyp" && contains([]string{"heic", "heix", "hevc", "hevx"}, string(b[8:12])) {
		return "image/heic"
	}
	return ""
}

// HTTPInProcess tests handler encoding and limits without opening a network port.
type HTTPInProcess struct{ Remote *Service }

func (t HTTPInProcess) call(ctx context.Context, path string, q, out any) error {
	b, e := json.Marshal(q)
	if e != nil {
		return e
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)).WithContext(ctx)
	w := httptest.NewRecorder()
	t.Remote.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		var body map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["error"] == "update_needed" {
			return ErrUpdateNeeded
		}
		return fmt.Errorf("sync HTTP %d", w.Code)
	}
	return json.Unmarshal(w.Body.Bytes(), out)
}
func (t HTTPInProcess) Pull(ctx context.Context, q PullRequest) (out PullResponse, e error) {
	e = t.call(ctx, "/sync/pull", q, &out)
	return
}
func (t HTTPInProcess) Push(ctx context.Context, q PushRequest) (out PushResponse, e error) {
	e = t.call(ctx, "/sync/push", q, &out)
	return
}
func (t HTTPInProcess) Blob(ctx context.Context, h string) ([]byte, error) {
	return t.Remote.Blob(ctx, h)
}
