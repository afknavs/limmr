package circles

import (
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestTaskRulesCircleWarningAndOverride(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	s := &Service{Hub: h, People: p}
	for _, reason := range []string{"", "ongoing area", strings.Repeat("word ", 201)} {
		_, err := s.Create(nil, model.Circle{Name: "Workshop", Kind: "project", TaskOverrideReason: reason}, "Current context")
		if !errors.Is(err, model.ErrConflict) {
			t.Fatalf("invalid override accepted: %v", err)
		}
	}
	var n int
	testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='circles' AND json_extract(data,'$.name')='Workshop'").Scan(&n))
	if n != 0 {
		t.Fatal("warning created a circle")
	}
	reason := "Recurring workshop community across projects"
	c, err := s.Create(nil, model.Circle{Name: "Workshop", Kind: "project", TaskOverrideReason: reason}, "Current context")
	testkit.Must(t, err)
	var stored model.Circle
	testkit.Must(t, h.Repo.Get("circles", c.ID, &stored))
	if stored.TaskOverrideReason != reason {
		t.Fatal("override reason not retained")
	}
	_, err = s.Create(nil, model.Circle{Name: "Close friends", Kind: "friends"}, "Lasting area")
	testkit.Must(t, err)
}

func TestTaskRulesBriefAskExpiryAnswerAndRenewal(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	s := &Service{Hub: h, People: p}
	c := model.Circle{ID: "ask-circle"}
	original := model.Brief{CircleID: c.ID, Version: 1, WrittenAt: now, OpenAsks: []model.OpenAsk{{Text: "Choose the venue", At: now}}}
	testkit.Must(t, h.Repo.Put("briefs", "ask-circle:1", original))
	read := func(want int) model.OpenAsk {
		t.Helper()
		var b model.Brief
		testkit.Must(t, h.Repo.Get("briefs", "ask-circle:1", &b))
		testkit.Must(t, s.presentAsks(c, &b))
		if len(b.OpenAsks) != want {
			t.Fatalf("asks=%d want %d", len(b.OpenAsks), want)
		}
		if want > 0 {
			return b.OpenAsks[0]
		}
		return model.OpenAsk{}
	}
	now = now.Add(model.NeedsYouTTL - time.Second)
	a := read(1)
	now = now.Add(time.Second)
	read(0)
	var stored model.Brief
	testkit.Must(t, h.Repo.Get("briefs", "ask-circle:1", &stored))
	if dbutil.JSON(stored) != dbutil.JSON(original) {
		t.Fatal("expiry changed evidence")
	}
	// A new version/follow-up has a fresh request time; answering hides only that version.
	stored.Version = 2
	stored.OpenAsks[0].At = now
	testkit.Must(t, h.Repo.Put("briefs", "ask-circle:1", stored))
	read(1)
	testkit.Must(t, h.Repo.Put("done_asks", c.ID+":"+a.ID, 2))
	read(0)
	stored.Version = 3
	stored.OpenAsks[0].At = now.Add(time.Minute)
	testkit.Must(t, h.Repo.Put("briefs", "ask-circle:1", stored))
	read(1)
}
