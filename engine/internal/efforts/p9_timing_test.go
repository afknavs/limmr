package efforts

import (
	"limmr/internal/testkit"
	"testing"
	"time"
)

// Same harness is run against pristine E6 and P9. Setup is outside each timer.
func TestP9MixedTimings(t *testing.T) {
	all, tasks := round2MixedCorpus()
	for i := 0; i < 3; i++ {
		s := qualitySeed(t, nil)
		putRound2(t, s, all)
		start := time.Now()
		_, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
		testkit.Must(t, err)
		cold := time.Since(start)
		m := tasks[2].ms[len(tasks[2].ms)-1]
		m.ID = "zz-p9-arrival"
		m.Timestamp = testkit.Epoch
		m.Content = "Juniper pitch pricing is agreed"
		m.QuotedMessageID = tasks[2].ms[0].ID
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
		start = time.Now()
		_, err = s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
		testkit.Must(t, err)
		t.Logf("sample=%d messages=%d cold_ms=%.3f incremental_ms=%.3f", i, len(all), float64(cold)/float64(time.Millisecond), float64(time.Since(start))/float64(time.Millisecond))
	}
}
