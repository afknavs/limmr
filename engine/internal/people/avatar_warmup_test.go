package people_test

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestN2PhotoWarmupRecentFirstGroupsLimiterAndWeeklyMisses(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Phone, Name: "Maya Rao", LastMessageTime: &now}))
	older := now.Add(-time.Hour)
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Group, Name: "Design Club", LastMessageTime: &older}))
	fake := &avatarFake{} // misses exercise the seven-day negative cache
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	var err error
	online := false
	connected := func() bool { return online }
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 0 {
		t.Fatal("offline lookup")
	}
	online = true
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 1 || fake.calls[0] != testkit.Phone {
		t.Fatal("recent person not first", fake.calls)
	}
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 1 {
		t.Fatal("bypassed global limiter")
	}
	now = now.Add(2 * time.Second)
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 2 || fake.calls[1] != testkit.Group {
		t.Fatal("group not warmed", fake.calls)
	}
	now = now.Add(6 * 24 * time.Hour)
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 2 {
		t.Fatal("early negative retry")
	}
	now = now.Add(24 * time.Hour)
	_, err = cache.WarmNext(t.Context(), connected)
	testkit.Must(t, err)
	if len(fake.calls) != 3 || fake.calls[2] != testkit.Phone {
		t.Fatal("miss did not retry after seven days")
	}
}

type cancelPhotoFake struct{ started chan struct{} }

func (f *cancelPhotoFake) FetchAvatar(ctx context.Context, _ string) ([]byte, error) {
	close(f.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestN2PhotoWarmupCancellationStopsAndDoesNotCacheShutdownAsMiss(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	fake := &cancelPhotoFake{started: make(chan struct{})}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := cache.StartWarmup(ctx, func() bool { return true }, nil)
	select {
	case <-fake.started:
	case <-time.After(time.Second):
		t.Fatal("did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	// A fresh fake may immediately retry once the global two-second limiter elapses.
	h.Now = func() time.Time { return testkit.Epoch.Add(2 * time.Second) }
	var err error
	next := &avatarFake{data: jpegFixture(t)}
	cache.Lookup = next
	_, err = cache.WarmNext(t.Context(), func() bool { return true })
	testkit.Must(t, err)
	if len(next.calls) != 1 {
		t.Fatal("shutdown became a seven-day miss")
	}
}

func TestN2PhotoWarmupSuccessIsNotRefetched(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	var err error
	for i := 0; i < 3; i++ {
		h.Now = func() time.Time { return testkit.Epoch.Add(time.Duration(i) * 8 * 24 * time.Hour) }
		_, err = cache.WarmNext(t.Context(), func() bool { return true })
		testkit.Must(t, err)
	}
	if len(fake.calls) != 2 {
		t.Fatal("expected one person and one group, once each", fake.calls)
	}
}
