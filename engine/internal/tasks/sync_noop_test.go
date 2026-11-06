package tasks

import (
	"context"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestUnchangedTicksKeepTaskTimestampAndHistory(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	testkit.Must(t, s.Tick(context.Background()))
	before, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	var count, after int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log WHERE bucket='tasks'`).Scan(&count))
	*now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		testkit.Must(t, s.Tick(context.Background()))
	}
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	testkit.Must(t, s.Hub.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log WHERE bucket='tasks'`).Scan(&after))
	if count != after || !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("unchanged tick rewrote task")
	}
}
