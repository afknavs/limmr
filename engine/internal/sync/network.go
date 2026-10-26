package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// HTTPTransport never follows redirects with a bearer credential.
type HTTPTransport struct {
	PeerURL, Token string
	Client         *http.Client
}

func (t HTTPTransport) client() *http.Client {
	c := http.Client{Timeout: 20 * 1e9}
	if t.Client != nil {
		c = *t.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}
func (t HTTPTransport) call(ctx context.Context, path string, q any) ([]byte, error) {
	b, e := json.Marshal(q)
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", t.PeerURL+path, bytes.NewReader(b))
	if e != nil {
		return nil, ErrInvalid
	}
	r.Header.Set("Authorization", "Bearer "+t.Token)
	r.Header.Set("Content-Type", "application/json")
	resp, e := t.client().Do(r)
	if e != nil {
		return nil, fmt.Errorf("sync peer unavailable")
	}
	defer resp.Body.Close()
	b, e = io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if e != nil {
		return nil, fmt.Errorf("sync peer connection dropped mid-response")
	}
	if len(b) > MaxBytes {
		return nil, ErrInvalid
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 409 {
			return nil, ErrUpdateNeeded
		}
		return nil, fmt.Errorf("sync peer returned HTTP %d", resp.StatusCode)
	}
	return b, nil
}
func (t HTTPTransport) Pull(c context.Context, q PullRequest) (v PullResponse, e error) {
	b, e := t.call(c, "/sync/pull", q)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return
}
func (t HTTPTransport) Push(c context.Context, q PushRequest) (v PushResponse, e error) {
	b, e := t.call(c, "/sync/push", q)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return
}
func (t HTTPTransport) Blob(c context.Context, h string) ([]byte, error) {
	return t.call(c, "/sync/blob", map[string]string{"sha256": h})
}

// Photos travel in bounded push chunks: the listening Mac never needs an inbound
// route to the dot. A partial upload is private and never becomes an avatar.
type BlobChunk struct {
	SHA256 string `json:"sha256"`
	Offset int64  `json:"offset"`
	Total  int64  `json:"total"`
	Data   []byte `json:"data"`
}

func (s *Service) receiveBlob(c *BlobChunk) error {
	h, e := hex.DecodeString(c.SHA256)
	if e != nil || len(h) != 32 || hex.EncodeToString(h) != c.SHA256 || c.Offset < 0 || c.Total < 1 || c.Total > MaxBytes || len(c.Data) == 0 || len(c.Data) > 512<<10 || c.Offset+int64(len(c.Data)) > c.Total {
		return ErrInvalid
	}
	root, e := os.OpenRoot(s.Home)
	if e != nil {
		return e
	}
	defer root.Close()
	if e = root.MkdirAll("avatars/custom", 0700); e != nil {
		return e
	}
	name := "avatars/custom/.upload-" + c.SHA256
	flags := os.O_WRONLY | os.O_CREATE
	if c.Offset == 0 {
		flags |= os.O_TRUNC
	}
	// Never follow a leaf symlink even within the scoped root.
	if fi, e := root.Lstat(name); e == nil && !fi.Mode().IsRegular() {
		return ErrInvalid
	}
	f, e := root.OpenFile(name, flags, 0600)
	if e != nil {
		return e
	}
	fi, e := f.Stat()
	if e != nil || fi.Size() != c.Offset {
		f.Close()
		return ErrInvalid
	}
	_, e = f.WriteAt(c.Data, c.Offset)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if c.Offset+int64(len(c.Data)) != c.Total {
		return nil
	}
	defer root.Remove(name)
	f, e = root.Open(name)
	if e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	f.Close()
	if e != nil {
		return e
	}
	sum := sha256.Sum256(b)
	if int64(len(b)) != c.Total || hex.EncodeToString(sum[:]) != c.SHA256 || blobType(b) == "" {
		return ErrInvalid
	}
	return storePhoto(root, c.SHA256, b)
}
func (t HTTPTransport) Upload(ctx context.Context, s *Service) error {
	hashes, e := s.profilePhotoHashes()
	if e != nil {
		return e
	}
	for _, h := range hashes {
		b, e := s.Blob(ctx, h)
		if e != nil {
			return e
		}
		for offset := 0; offset < len(b); {
			end := min(offset+(512<<10), len(b))
			_, e = t.Push(ctx, PushRequest{FromEngineID: s.EngineID, SyncVersion: Version, Schema: Schema, Blob: &BlobChunk{h, int64(offset), int64(len(b)), b[offset:end]}})
			if e != nil {
				return e
			}
			offset = end
		}
	}
	return nil
}
