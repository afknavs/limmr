package tasks

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func coverTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	testkit.Must(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
	return b.Bytes()
}

type coverFake struct {
	wa.Fake      // Includes the send recorder: cover work must leave it empty.
	service      *Service
	connected    bool
	fail         bool
	calls        []string
	active, peak int
	delay        time.Duration
	started      chan struct{}
	mu           sync.Mutex
}

func (f *coverFake) Connected() bool { return f.connected }
func (f *coverFake) Download(ctx context.Context, chat, id string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, chat+":"+id)
	f.active++
	f.peak = max(f.peak, f.active)
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	if f.fail {
		return "", fmt.Errorf("invented failure")
	}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 32, 40)))
	path := filepath.Join(f.service.Hub.Home, "media", dbutil.Digest(chat+"\x00"+id))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b.Bytes(), 0600)
}

// 30 independent tasks x 50 images = 1,500; 60 thumbnails = 4%.
// The ten newest tasks have a thumbnail on the newest image but no original.
func sparseCoverFixture(t *testing.T) (*Service, *coverFake, []Task) {
	t.Helper()
	s, _, now := fixture(t)
	tasks := []Task{}
	for i := 0; i < 30; i++ {
		testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: fmt.Sprintf("155501%05d@s.whatsapp.net", i), Name: fmt.Sprintf("Invented chat %02d", i)}))
		task, err := s.Create(nil, Create{Title: fmt.Sprintf("Invented task %02d", i), Kind: "long", Chats: []Entity{{ID: fmt.Sprintf("155501%05d@s.whatsapp.net", i)}}})
		testkit.Must(t, err)
		_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.last_active_at',?) WHERE id=?", now.Add(time.Duration(i)*time.Minute).Format(time.RFC3339Nano), task.ID)
		testkit.Must(t, err)
		tasks = append(tasks, task)
	}
	pixels := coverTestPNG(t, 24, 30)
	tx, err := s.Hub.Messages.DB.Begin()
	testkit.Must(t, err)
	defer tx.Rollback()
	for i, task := range tasks {
		for j := 0; j < 50; j++ {
			var thumb []byte
			if i < 20 && (j == 5 || j == 15) || i >= 20 && (j == 25 || j == 49) {
				thumb = pixels
			}
			_, err = tx.Exec("INSERT INTO messages(id,chat_jid,sender,content,is_from_me,timestamp,media_type,preview_thumbnail) VALUES(?,?,?,'',0,?,'image',?)", fmt.Sprintf("image-%02d", j), task.Chats[0].ID, task.Chats[0].ID, now.Add(time.Duration(j)*time.Minute), thumb)
			testkit.Must(t, err)
		}
	}
	testkit.Must(t, tx.Commit())
	fake := &coverFake{service: s, connected: true}
	s.CoverMedia = fake
	return s, fake, tasks
}
func pictureCount(t *testing.T, s *Service, tasks []Task) int {
	t.Helper()
	n := 0
	for _, task := range tasks {
		if c := s.coverCandidate(nil, task); c != nil && s.coverHasPixels(c) {
			preview, err := s.CoverPreview(nil, task.ID)
			testkit.Must(t, err)
			if regularFile(preview["path"].(string)) {
				n++
			}
		}
	}
	return n
}
func TestN2SparsePixelsBeforeAfterAndRecentDownloads(t *testing.T) {
	s, f, tasks := sparseCoverFixture(t)
	var images, thumbs int
	testkit.Must(t, s.Hub.Messages.DB.QueryRow("SELECT count(*),count(preview_thumbnail) FROM messages WHERE media_type='image'").Scan(&images, &thumbs))
	if images != 1500 || thumbs != 60 {
		t.Fatal(images, thumbs)
	}
	before := 0
	for _, task := range tasks {
		if c := s.selectCoverCandidate(nil, task, false); c != nil && s.coverHasPixels(c) {
			before++
		}
	}
	selected := pictureCount(t, s, tasks)
	f.delay = 2 * time.Millisecond
	var passes sync.WaitGroup
	for i := 0; i < 2; i++ {
		passes.Add(1)
		go func() { defer passes.Done(); s.fetchRecentCovers(context.Background()) }()
	}
	passes.Wait()
	after := pictureCount(t, s, tasks)
	t.Logf("1,500 images; 60 stored thumbnails (4%%); picture tasks: old selector %d/30 -> pixel selector %d/30 -> fetch %d/30", before, selected, after)
	if before != 10 || selected != 30 || after != 30 || len(f.calls) != 10 || len(f.Sent) != 0 || f.peak != 1 {
		t.Fatal(before, selected, after, len(f.calls), len(f.Sent))
	}
	for _, task := range tasks[20:] {
		if c := s.coverCandidate(nil, task); c == nil || !s.coverHasFile(c) {
			t.Fatal("recent cover still uses embedded pixels", c)
		}
	}
	for _, call := range f.calls {
		if !bytes.HasSuffix([]byte(call), []byte(":image-49")) {
			t.Fatal("not best image", call)
		}
	}
}
func TestN2CoverAttemptPersistsFailureAndNextDay(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "missing", ChatJID: testkit.Phone, MediaType: "image", Timestamp: *now}))
	f := &coverFake{service: s, connected: false, fail: true}
	s.CoverMedia = f
	s.fetchRecentCovers(context.Background())
	if len(f.calls) != 0 {
		t.Fatal("offline download")
	}
	f.connected = true
	// Concurrent passes and a fresh service share the persistent attempt cap.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.fetchRecentCovers(context.Background()) }()
	}
	wg.Wait()
	restarted := New(s.Hub, s.Efforts)
	restarted.CoverMedia = f
	restarted.fetchRecentCovers(context.Background())
	if len(f.calls) != 1 {
		t.Fatal("daily retry", len(f.calls))
	}
	*now = now.Add(24 * time.Hour)
	f.fail = false
	restarted.fetchRecentCovers(context.Background())
	if len(f.calls) != 2 || pictureCount(t, s, []Task{task}) != 1 {
		t.Fatal("next day not retried")
	}
}
func TestN2CoverWorkerCancelsInFlightOnShutdown(t *testing.T) {
	s, a, now := fixture(t)
	create(t, s, a, "long")
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "blocked", ChatJID: testkit.Phone, MediaType: "image", Timestamp: *now}))
	f := &coverFake{service: s, connected: true, started: make(chan struct{}, 1)}
	s.CoverMedia = f
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.runCoverDownloads(ctx) }()
	select {
	case <-f.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if len(f.calls) != 1 || len(f.Sent) != 0 {
		t.Fatal("unexpected actions")
	}
}
func TestN2OwnerMissingFetchedAndRevisionChanges(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	for _, id := range []string{"chosen", "available"} {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: id, ChatJID: testkit.Phone, MediaType: "image", Timestamp: *now}))
	}
	coverOriginal(t, s, testkit.Phone, "available", 32, 40)
	item := Item{ID: "owner", Kind: "file", MediaType: "image", MessageRef: &Ref{ChatJID: testkit.Phone, MessageID: "chosen"}}
	_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?),'$.cover_item_id','owner') WHERE id=?", dbutil.JSON([]Item{item}), task.ID)
	testkit.Must(t, err)
	task.CoverItemID = "owner"
	before := s.coverCandidate(nil, task)
	if before == nil || before.MediaID != "chosen" || s.coverHasPixels(before) {
		t.Fatal("missing owner not retained")
	}
	f := &coverFake{service: s, connected: true}
	s.CoverMedia = f
	s.fetchRecentCovers(context.Background())
	after := s.coverCandidate(nil, task)
	if len(f.calls) != 1 || !s.coverHasPixels(after) || after.ThumbnailURL == before.ThumbnailURL {
		t.Fatal("download/revision missing")
	}
	// Owner's explicit removal suppresses background fetching too.
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.cover_removed',json('true')) WHERE id=?", task.ID)
	testkit.Must(t, err)
	s.fetchRecentCovers(context.Background())
	if len(f.calls) != 1 {
		t.Fatal("owner removal downloaded")
	}
}
func TestN2BoundedCachedThumbnails(t *testing.T) {
	for _, size := range [][2]int{{2400, 1200}, {1200, 2400}, {80, 100}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, a, now := fixture(t)
			task := create(t, s, a, "long")
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "large", ChatJID: testkit.Phone, MediaType: "image", Timestamp: *now}))
			coverOriginal(t, s, testkit.Phone, "large", size[0], size[1])
			source := s.coverMediaPath(&CoverCandidate{ChatJID: testkit.Phone, MediaID: "large"})
			original, err := os.ReadFile(source)
			testkit.Must(t, err)
			testkit.Must(t, os.WriteFile(source+".cover-600.jpg", coverTestPNG(t, 20, 10), 0600))
			preview, err := s.CoverPreview(nil, task.ID)
			testkit.Must(t, err)
			path := preview["path"].(string)
			if path == source || filepath.Dir(path) != filepath.Dir(source) {
				t.Fatal("original returned")
			}
			data, err := os.ReadFile(path)
			testkit.Must(t, err)
			config, _, err := image.DecodeConfig(bytes.NewReader(data))
			testkit.Must(t, err)
			if max(config.Width, config.Height) != min(1280, max(size[0], size[1])) || config.Width*size[1] != config.Height*size[0] || !strings.HasSuffix(path, ".cover-1280.jpg") {
				t.Fatal("dimensions", config)
			}
			stat, err := os.Stat(path)
			testkit.Must(t, err)
			_, err = s.CoverPreview(nil, task.ID)
			testkit.Must(t, err)
			cached, err := os.Stat(path)
			testkit.Must(t, err)
			unchanged, err := os.ReadFile(source)
			testkit.Must(t, err)
			if !cached.ModTime().Equal(stat.ModTime()) || !bytes.Equal(original, unchanged) {
				t.Fatal("cache/original changed")
			}
		})
	}
}

func TestN2WorkspaceAndChatSkipMissingPixels(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	items := []Item{}
	for i, id := range []string{"old-workspace", "new-workspace", "missing-workspace"} {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: id, ChatJID: testkit.Phone, MediaType: "image", Timestamp: now.Add(time.Duration(i) * time.Minute)}))
		items = append(items, Item{ID: id, Kind: "file", MediaType: "image", AddedAt: now.Add(time.Duration(i) * time.Minute), MessageRef: &Ref{ChatJID: testkit.Phone, MessageID: id}})
	}
	items = append(items, Item{ID: "link", Kind: "link", URL: "https://example.invalid/guide"})
	_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", dbutil.JSON(items), task.ID)
	testkit.Must(t, err)
	for _, id := range []string{"old-workspace", "new-workspace"} {
		coverOriginal(t, s, testkit.Phone, id, 40, 50)
	}
	if c := s.coverCandidate(nil, task); c == nil || c.ItemID != "new-workspace" {
		t.Fatal("newest drawable workspace missing", c)
	}
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", dbutil.JSON(items[3:]), task.ID)
	testkit.Must(t, err)
	if c := s.coverCandidate(nil, task); c == nil || c.MediaID != "new-workspace" {
		t.Fatal("newest drawable chat missing", c)
	}
	for _, id := range []string{"old-workspace", "new-workspace"} {
		testkit.Must(t, os.Remove(s.coverMediaPath(&CoverCandidate{ChatJID: testkit.Phone, MediaID: id})))
	}
	if c := s.coverCandidate(nil, task); c == nil || c.ItemID != "link" || c.LinkURL == "" {
		t.Fatal("missing images blocked link", c)
	}
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json('[]')) WHERE id=?", task.ID)
	testkit.Must(t, err)
	if c := s.coverCandidate(nil, task); c != nil {
		t.Fatal("missing images blocked typography", c)
	}
	f := &coverFake{service: s, connected: true}
	s.CoverMedia = f
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, s.Hub.SetSettings(settings))
	s.fetchRecentCovers(context.Background())
	settings.HiddenChats = nil
	testkit.Must(t, s.Hub.SetSettings(settings))
	_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE media_type='image'", *now)
	testkit.Must(t, err)
	s.fetchRecentCovers(context.Background())
	if len(f.calls) != 0 {
		t.Fatal("invisible source downloaded")
	}
}

func TestVideoWorkspacePrefersFileThenNewestThumbnail(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	items := []Item{}
	for i, id := range []string{"older-file", "newer-file", "newest-thumb"} {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: id, ChatJID: testkit.Phone, MediaType: "image", Timestamp: now.Add(time.Duration(i) * time.Minute)}))
		_, err := s.Hub.Messages.DB.Exec("UPDATE messages SET preview_thumbnail=? WHERE id=?", coverTestPNG(t, 80, 100), id)
		testkit.Must(t, err)
		items = append(items, Item{ID: id, Kind: "file", MediaType: "image", AddedAt: now.Add(time.Duration(i) * time.Minute), MessageRef: &Ref{ChatJID: testkit.Phone, MessageID: id}})
	}
	_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", dbutil.JSON(items), task.ID)
	testkit.Must(t, err)
	for _, id := range []string{"older-file", "newer-file"} {
		coverOriginal(t, s, testkit.Phone, id, 80, 100)
	}
	if c := s.coverCandidate(nil, task); c == nil || c.ItemID != "newer-file" {
		t.Fatal("workspace did not prefer newest file", c)
	}
	for _, id := range []string{"older-file", "newer-file"} {
		testkit.Must(t, os.Remove(s.coverMediaPath(&CoverCandidate{ChatJID: testkit.Phone, MediaID: id})))
	}
	if c := s.coverCandidate(nil, task); c == nil || c.ItemID != "newest-thumb" || !s.coverHasPixels(c) || s.coverHasFile(c) {
		t.Fatal("workspace thumbnail fallback missing", c)
	}
}

func TestVideoChatScanBoundedToFiftyNewestImages(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	for i := 0; i < 51; i++ {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("photo-%02d", i), ChatJID: testkit.Phone, MediaType: "image", Timestamp: now.Add(time.Duration(i) * time.Minute)}))
	}
	coverOriginal(t, s, testkit.Phone, "photo-00", 80, 100)
	if c := s.coverCandidate(nil, task); c != nil {
		t.Fatal("scanned beyond fifty newest images", c)
	}
	_, err := s.Hub.Messages.DB.Exec("UPDATE messages SET preview_thumbnail=? WHERE id='photo-50'", coverTestPNG(t, 80, 100))
	testkit.Must(t, err)
	if c := s.coverCandidate(nil, task); c == nil || c.MediaID != "photo-50" {
		t.Fatal("bounded thumbnail fallback missing", c)
	}
	coverOriginal(t, s, testkit.Phone, "photo-01", 80, 100)
	if c := s.coverCandidate(nil, task); c == nil || c.MediaID != "photo-01" {
		t.Fatal("fiftieth file not preferred", c)
	}
}

func TestVideoPackedPixelsMatchImageInterface(t *testing.T) {
	bounds := image.Rect(3, 5, 11, 13)
	images := []image.Image{image.NewRGBA(bounds), image.NewNRGBA(bounds), image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420), image.NewGray(bounds), image.NewRGBA64(bounds)}
	for _, src := range images {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				switch im := src.(type) {
				case *image.RGBA:
					im.Set(x, y, color.NRGBA{R: 120, G: 70, B: 35, A: uint8(x * 20)})
				case *image.NRGBA:
					im.SetNRGBA(x, y, color.NRGBA{R: 120, G: 70, B: 35, A: uint8(x * 20)})
				case *image.YCbCr:
					im.Y[im.YOffset(x, y)] = uint8(x * 20)
					im.Cb[im.COffset(x, y)] = 100
					im.Cr[im.COffset(x, y)] = 180
				case *image.Gray:
					im.SetGray(x, y, color.Gray{Y: uint8(x * 20)})
				}
				r, g, b, a := coverRGBA(src, x, y)
				rr, gg, bb, aa := src.At(x, y).RGBA()
				if r != rr || g != gg || b != bb || a != aa {
					t.Fatalf("%T packed pixel differs at %d,%d", src, x, y)
				}
			}
		}
	}
}
