package hub

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChunkUploadSixMiBResumeAndSendCopy(t *testing.T) {
	home := t.TempDir()
	r, e := Open(filepath.Join(home, "hub.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	now := time.Now()
	s := New(r, nil, home)
	s.Now = func() time.Time { return now }
	data := bytes.Repeat([]byte("invented poster "), (6<<20)/16)
	copy(data, []byte("%PDF-1.7\n"))
	count := (len(data) + MaxChunkDecoded - 1) / MaxChunkDecoded
	var u Upload
	for i := 0; i < count; i++ {
		encoded := base64.StdEncoding.EncodeToString(data[i*MaxChunkDecoded : min((i+1)*MaxChunkDecoded, len(data))])
		u, e = s.ChunkUpload("ai", u.ID, "poster.pdf", count, int64(len(data)), &i, &encoded)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			if _, e = s.ChunkUpload("other", u.ID, "", 0, 0, nil, nil); e == nil {
				t.Fatal("cross-agent resume")
			}
			s = New(r, nil, home)
			s.Now = func() time.Time { return now }
			u, e = s.ChunkUpload("ai", u.ID, "", 0, 0, nil, nil)
			if e != nil || u.Bytes != MaxChunkDecoded || len(u.Missing) != count-1 {
				t.Fatalf("resume: %+v %v", u, e)
			}
			u, e = s.ChunkUpload("ai", u.ID, "", 0, 0, &i, &encoded)
			if e != nil || u.Bytes != MaxChunkDecoded {
				t.Fatal("duplicate chunk", e)
			}
		}
	}
	b, e := os.ReadFile(filepath.Join(home, "outbox", u.AttachmentID))
	if e != nil || !bytes.Equal(data, b) || u.Status != "uploaded" {
		t.Fatal("outbox mismatch", e)
	}
	u, e = s.ChunkUpload("ai", u.ID, "", 0, 0, nil, nil)
	if e != nil || u.AttachmentID == "" {
		t.Fatal("completed resume", e)
	}
	now = now.Add(time.Hour)
	if _, e = s.ChunkUpload("ai", u.ID, "", 0, 0, nil, nil); e == nil {
		t.Fatal("expiry")
	}
}
func TestChunkBoundsAndConflictingRetry(t *testing.T) {
	home := t.TempDir()
	r, e := Open(filepath.Join(home, "hub.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	s := New(r, nil, home)
	i := 0
	b := base64.StdEncoding.EncodeToString([]byte("one"))
	u, e := s.ChunkUpload("ai", "", "poster.pdf", 2, 6, &i, &b)
	if e != nil {
		t.Fatal(e)
	}
	changed := base64.StdEncoding.EncodeToString([]byte("two"))
	if _, e = s.ChunkUpload("ai", u.ID, "", 0, 0, &i, &changed); e == nil {
		t.Fatal("conflicting retry")
	}
	huge := string(bytes.Repeat([]byte("A"), MaxChunkBase64+1))
	if _, e = s.ChunkUpload("ai", u.ID, "", 0, 0, &i, &huge); e == nil {
		t.Fatal("chunk cap")
	}
	if _, e = s.ChunkUpload("ai", "", "poster.pdf", 256, 101<<20, &i, &b); e == nil {
		t.Fatal("total cap")
	}
	for n := 0; n < 3; n++ {
		if _, e = s.ChunkUpload("ai", "", "poster.pdf", 2, 6, &i, &b); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.ChunkUpload("ai", "", "poster.pdf", 2, 6, &i, &b); e == nil {
		t.Fatal("active cap")
	}
}
