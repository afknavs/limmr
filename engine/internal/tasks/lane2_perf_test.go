package tasks

import (
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

// Fixed synthetic workload; go test -run TestLane2NavigationProfile -v -count=1.
func TestLane2NavigationProfile(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	tx, err := s.Hub.Messages.DB.Begin()
	testkit.Must(t, err)
	for i := 0; i < 6000; i++ {
		_, err = tx.Exec("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me) VALUES(?,?,?,?,?,0)", fmt.Sprintf("perf-%05d", i), testkit.Phone, testkit.Phone, "Synthetic update https://example.com/file", now.Add(time.Duration(i)*time.Second))
		testkit.Must(t, err)
	}
	testkit.Must(t, tx.Commit())
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	for i := 0; i < 80; i++ {
		r.Items = append(r.Items, Item{ID: fmt.Sprint(i), Kind: "note", Title: "Synthetic file", Text: strings.Repeat("Synthetic note. ", 50), Source: "ai"})
	}
	tx, err = s.Hub.Repo.DB.Begin()
	testkit.Must(t, err)
	for i := 0; i < 400; i++ {
		r.ID = fmt.Sprintf("perf-%04d", i)
		_, err = tx.Exec("INSERT INTO tasks VALUES(?,?)", r.ID, dbutil.JSON(r))
		testkit.Must(t, err)
	}
	testkit.Must(t, tx.Commit())
	for _, measure := range []struct {
		name string
		f    func() error
	}{
		{"home_task_list_400", func() error { _, e := s.List(nil, "open", 500); return e }},
		{"task_page_6000_messages", func() error { _, e := s.Get(nil, v.ID); return e }},
	} {
		start := time.Now()
		for i := 0; i < 12; i++ {
			testkit.Must(t, measure.f())
		}
		t.Logf("%s mean=%.3fms", measure.name, float64(time.Since(start).Microseconds())/12000)
	}
}
