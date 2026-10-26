package people_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type avatarFake struct {
	calls []string
	data  []byte
}

func (f *avatarFake) FetchAvatar(_ context.Context, j string) ([]byte, error) {
	f.calls = append(f.calls, j)
	return f.data, nil
}
func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	testkit.Must(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil))
	return b.Bytes()
}
func TestAvatarCacheWeeklyAndGlobalRateLimit(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	ctx := context.Background()
	data, e := cache.Get(ctx, id)
	testkit.Must(t, e)
	if !bytes.Equal(data, fake.data) || len(fake.calls) != 1 || fake.calls[0] != testkit.Phone {
		t.Fatal("lookup")
	}
	// Cache survives a new service instance; metadata and global throttle persist.
	cache = &people.AvatarCache{Hub: h, Lookup: fake}
	_, e = cache.Get(ctx, id)
	testkit.Must(t, e)
	if len(fake.calls) != 1 {
		t.Fatal("duplicate lookup")
	}
	testkit.Must(t, p.PutPerson(model.Person{ID: "second", Name: "Noor Chen", JIDs: []string{"15550100002@s.whatsapp.net"}}))
	if _, e = cache.Get(ctx, "second"); e != model.ErrNotFound || len(fake.calls) != 1 {
		t.Fatal("global throttle")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(2 * time.Second) }
	_, e = cache.Get(ctx, "second")
	testkit.Must(t, e)
	if len(fake.calls) != 2 {
		t.Fatal("throttle boundary")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(7*24*time.Hour - time.Second) }
	_, e = cache.Get(ctx, id)
	testkit.Must(t, e)
	if len(fake.calls) != 2 {
		t.Fatal("weekly throttle")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(7 * 24 * time.Hour) }
	_, e = cache.Get(ctx, id)
	testkit.Must(t, e)
	if len(fake.calls) != 3 {
		t.Fatal("weekly refresh missing")
	}
	entries, e := os.ReadDir(filepath.Join(h.Home, "avatars"))
	testkit.Must(t, e)
	if len(entries) != 2 {
		t.Fatal("cache files")
	}
	for _, entry := range entries {
		st, e := entry.Info()
		testkit.Must(t, e)
		if st.Mode().Perm() != 0600 {
			t.Fatal("photo permissions")
		}
	}
	st, e := os.Stat(filepath.Join(h.Home, "avatars"))
	testkit.Must(t, e)
	if st.Mode().Perm() != 0700 {
		t.Fatal("directory permissions")
	}
}
func TestAvatarMissesAreCachedAndUnknownJIDsNeverLookedUp(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	fake := &avatarFake{}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	ctx := context.Background()
	id := model.PersonID(testkit.Phone)
	for _, v := range []model.Person{{ID: "no-jid", Name: "Noor"}, {ID: "group-jid", Name: "Group", JIDs: []string{testkit.Group}}} {
		testkit.Must(t, p.PutPerson(v))
	}
	for _, id := range []string{"unknown", testkit.Phone, "no-jid", "group-jid"} {
		if _, e := cache.Get(ctx, id); e != model.ErrNotFound {
			t.Fatal("unknown should return 404")
		}
	}
	if len(fake.calls) != 0 {
		t.Fatal("looked up unknown JID")
	}
	if _, e := cache.Get(ctx, id); e != model.ErrNotFound {
		t.Fatal("miss should return 404")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(24 * time.Hour) }
	if _, e := cache.Get(ctx, id); e != model.ErrNotFound || len(fake.calls) != 1 {
		t.Fatal("negative cache")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(7 * 24 * time.Hour) }
	if _, e := cache.Get(ctx, id); e != model.ErrNotFound || len(fake.calls) != 2 {
		t.Fatal("weekly negative refresh")
	}
	// Demo always misses even with a cached photo from another service.
	if _, e := (&people.AvatarCache{Hub: h}).Get(ctx, id); e != model.ErrNotFound {
		t.Fatal("demo avatar available")
	}
}
func TestAvatarConcurrentRequestsShareLookup(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.Get(context.Background(), model.PersonID(testkit.Phone)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(fake.calls) != 1 {
		t.Fatal("duplicate concurrent lookup")
	}
}
func TestAvatarRejectsInvalidImageAndOversize(t *testing.T) {
	for _, data := range [][]byte{[]byte("not a JPEG"), make([]byte, people.MaxAvatarBytes+1)} {
		h, p, _ := testkit.Open(t)
		testkit.Seed(t, h, p)
		cache := &people.AvatarCache{Hub: h, Lookup: &avatarFake{data: data}}
		if _, e := cache.Get(context.Background(), model.PersonID(testkit.Phone)); e != model.ErrNotFound {
			t.Fatal("invalid image accepted")
		}
	}
}
