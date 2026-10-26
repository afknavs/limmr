package sync_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"limmr/internal/model"
	"limmr/internal/people"
	syncer "limmr/internal/sync"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func profilePhotoFixture(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	must(t, jpeg.Encode(&out, img, nil))
	return out.Bytes()
}
func photoPeers(t *testing.T) (*engine, *engine, model.Person) {
	t.Helper()
	a, b := newEngine(t), newEngine(t)
	put(t, a, "people", "maya", model.Person{ID: "maya", Name: "Maya Rao", JIDs: []string{"15550000001@s.whatsapp.net"}})
	must(t, a.p.ImportContacts([]people.ContactMatch{{PersonID: "maya", Photo: profilePhotoFixture(t, color.RGBA{R: 255, A: 255})}}))
	must(t, a.p.SetPhoto("maya", profilePhotoFixture(t, color.RGBA{B: 255, A: 255})))
	var person model.Person
	must(t, a.h.Repo.Get("people", "maya", &person))
	if person.Photo.SHA256 == person.ContactsPhoto.SHA256 {
		t.Fatal("fixture photos must differ")
	}
	pair(t, a, b)
	exchange(t, a, b)
	return a, b, person
}
func TestSyncedProfilePhotosRenderWithoutNetwork(t *testing.T) {
	t.Run("selected_custom", func(t *testing.T) {
		a, b, _ := photoPeers(t)
		expected, err := (&people.AvatarCache{Hub: a.h}).Get(ctx, "maya")
		must(t, err)
		actual, err := (&people.AvatarCache{Hub: b.h}).Get(ctx, "maya")
		must(t, err)
		if !bytes.Equal(actual, expected) {
			t.Fatal("synced custom avatar differs")
		}
	})
	t.Run("contacts_fallback_available_offline", func(t *testing.T) {
		a, b, person := photoPeers(t)
		expected, err := a.s.Blob(ctx, person.ContactsPhoto.SHA256)
		must(t, err)
		actual, err := b.s.Blob(ctx, person.ContactsPhoto.SHA256)
		must(t, err)
		if !bytes.Equal(actual, expected) {
			t.Fatal("fallback bytes did not sync")
		}
		// Remove selected custom photo without another network exchange.
		must(t, b.p.SetPhoto("maya", nil))
		actual, err = (&people.AvatarCache{Hub: b.h}).Get(ctx, "maya")
		must(t, err)
		if !bytes.Equal(actual, expected) {
			t.Fatal("offline Contacts fallback unavailable")
		}
	})
}

type profilePhotoRoundTripper struct {
	handler http.Handler
	hashes  map[string]bool
}

func (r profilePhotoRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var q syncer.PushRequest
	if err = json.Unmarshal(raw, &q); err != nil {
		return nil, err
	}
	if q.Blob != nil {
		r.hashes[q.Blob.SHA256] = true
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	w := httptest.NewRecorder()
	r.handler.ServeHTTP(w, req)
	return w.Result(), nil
}

func TestOutboundUploadIncludesOnlyProfilePhotoReferences(t *testing.T) {
	a, b, person := photoPeers(t)
	// The sender also holds message media and an orphaned profile file. Neither is
	// part of the profile reference set and neither may be uploaded by knowledge sync.
	extra := profilePhotoFixture(t, color.RGBA{G: 255, A: 255})
	sum := sha256.Sum256(extra)
	extraHash := hex.EncodeToString(sum[:])
	must(t, os.MkdirAll(filepath.Join(a.home, "media"), 0700))
	must(t, os.WriteFile(filepath.Join(a.home, "media", extraHash), extra, 0600))
	must(t, os.WriteFile(filepath.Join(a.home, "avatars", "custom", extraHash), extra, 0600))
	hashes := map[string]bool{}
	tr := syncer.HTTPTransport{PeerURL: "https://fixture.invalid", Client: &http.Client{Transport: profilePhotoRoundTripper{handler: b.s.Handler(), hashes: hashes}}}
	must(t, tr.Upload(ctx, a.s))
	if len(hashes) != 2 || !hashes[person.Photo.SHA256] || !hashes[person.ContactsPhoto.SHA256] {
		t.Fatal("upload scope must be selected and fallback profile photos only")
	}
	if _, err := b.s.Blob(ctx, extraHash); !os.IsNotExist(err) {
		t.Fatal("unreferenced/media bytes traveled")
	}
}
