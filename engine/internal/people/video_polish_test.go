package people_test

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoLegacyAvatarsRefreshOnceAndKeepBytesOnFailure(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, group := range []bool{false, true} {
			for _, success := range []bool{false, true} {
				t.Run(fmt.Sprintf("warm=%v/group=%v/success=%v", warm, group, success), func(t *testing.T) {
					h, _, _ := testkit.Open(t)
					now := testkit.Epoch
					h.Now = func() time.Time { return now }
					id := model.PersonID(testkit.Phone)
					if group {
						id = testkit.Group
						testkit.Must(t, h.Repo.Put("groups", id, model.Group{JID: id, Name: "Invented group"}))
					} else {
						testkit.Must(t, h.Repo.Put("people", id, model.Person{ID: id, Name: "Invented person", JIDs: []string{testkit.Phone}}))
					}
					old := jpegFixture(t)
					path := filepath.Join(h.Home, "avatars", dbutil.Digest(id)+".jpg")
					testkit.Must(t, os.MkdirAll(filepath.Dir(path), 0700))
					testkit.Must(t, os.WriteFile(path, old, 0600))
					testkit.Must(t, h.Repo.Put("avatar_cache", id, map[string]any{"available": true, "attempted_at": now}))
					var b bytes.Buffer
					testkit.Must(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 640, 640)), nil))
					full := b.Bytes()
					fake := &avatarFake{}
					if success {
						fake.data = full
					}
					cache := &people.AvatarCache{Hub: h, Lookup: fake}
					if warm {
						attempted, err := cache.WarmNext(t.Context(), func() bool { return true })
						testkit.Must(t, err)
						if !attempted {
							t.Fatal("legacy preview not upgraded")
						}
					} else {
						data, err := cache.Get(t.Context(), id)
						testkit.Must(t, err)
						want := old
						if success {
							want = full
						}
						if !bytes.Equal(data, want) {
							t.Fatal("wrong foreground bytes")
						}
					}
					if len(fake.calls) != 1 {
						t.Fatal("expected one migration lookup", fake.calls)
					}
					now = now.Add(2 * time.Second)
					data, err := cache.Get(t.Context(), id)
					testkit.Must(t, err)
					want := old
					if success {
						want = full
					}
					if !bytes.Equal(data, want) || len(fake.calls) != 1 {
						t.Fatal("cached photo lost or immediate retry")
					}
					attempted, err := cache.WarmNext(t.Context(), func() bool { return true })
					testkit.Must(t, err)
					if attempted || len(fake.calls) != 1 {
						t.Fatal("warmup repeated migration")
					}
					disk, err := os.ReadFile(path)
					testkit.Must(t, err)
					if !bytes.Equal(disk, want) {
						t.Fatal("cached bytes replaced on failure")
					}
					var meta struct {
						Full          bool `json:"full"`
						FullAttempted bool `json:"full_attempted"`
						Available     bool `json:"available"`
					}
					testkit.Must(t, h.Repo.Get("avatar_cache", id, &meta))
					if meta.Full != success || !meta.FullAttempted || !meta.Available {
						t.Fatal("migration metadata", meta)
					}
					if !success {
						now = now.Add(7 * 24 * time.Hour)
						fake.data = full
						attempted, err = cache.WarmNext(t.Context(), func() bool { return true })
						testkit.Must(t, err)
						if !attempted || len(fake.calls) != 2 {
							t.Fatal("failed upgrade did not observe weekly retry")
						}
					}
				})
			}
		}
	}
}

func TestVideoLegacyAvatarUpgradeRespectsGlobalLimiter(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	id := model.PersonID(testkit.Phone)
	old := jpegFixture(t)
	path := filepath.Join(h.Home, "avatars", dbutil.Digest(id)+".jpg")
	testkit.Must(t, os.MkdirAll(filepath.Dir(path), 0700))
	testkit.Must(t, os.WriteFile(path, old, 0600))
	testkit.Must(t, h.Repo.Put("avatar_cache", id, map[string]any{"available": true, "attempted_at": now}))
	testkit.Must(t, h.Repo.Put("avatar_meta", "last_lookup", now))
	fake := &avatarFake{data: old}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	data, err := cache.Get(t.Context(), id)
	testkit.Must(t, err)
	if !bytes.Equal(data, old) || len(fake.calls) != 0 {
		t.Fatal("limiter lost old photo or allowed lookup")
	}
	now = now.Add(2 * time.Second)
	_, err = cache.Get(t.Context(), id)
	testkit.Must(t, err)
	if len(fake.calls) != 1 {
		t.Fatal("upgrade not retried at limiter boundary")
	}
}

func TestVideoIdleWarmPassOnlyStatsCustomPhotos(t *testing.T) {
	h, _, _ := testkit.Open(t)
	// A stat-able but unreadable/undecodable payload proves WarmNext does not load
	// or decode these bytes. Foreground Get remains responsible for validation.
	hash := dbutil.Digest("invented custom photo")
	dir := filepath.Join(h.Home, "avatars", "custom")
	testkit.Must(t, os.MkdirAll(dir, 0700))
	testkit.Must(t, os.WriteFile(filepath.Join(dir, hash+".jpg"), []byte("not decoded by warmup"), 0000))
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, h.Repo.Put("people", id, model.Person{ID: id, Name: "Invented person", JIDs: []string{testkit.Phone}, Photo: &model.PhotoReference{SHA256: hash}}))
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	attempted, err := cache.WarmNext(t.Context(), func() bool { return true })
	testkit.Must(t, err)
	if attempted || len(fake.calls) != 0 {
		t.Fatal("idle pass read/decoded photo or performed lookup")
	}
}
