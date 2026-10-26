package triggers

import (
	"testing"
	"time"
)

func TestR4QuietHoursDSTAndTimezoneChange(t *testing.T) {
	for _, tc := range []struct {
		at, zone string
		want     bool
	}{
		// Spring jumps over 02:00; autumn repeats 01:00. Both instances of
		// a repeated local quiet hour must apply the same send policy.
		{"2026-03-08T06:59:00Z", "America/New_York", true},
		{"2026-03-08T07:00:00Z", "America/New_York", false},
		{"2026-11-01T05:30:00Z", "America/New_York", true},
		{"2026-11-01T06:30:00Z", "America/New_York", true},
		{"2026-11-01T08:00:00Z", "America/New_York", false},
		{"2026-11-01T06:30:00Z", "Asia/Kolkata", false},
	} {
		t.Run(tc.at+tc.zone, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			must(t, err)
			got, err := inQuiet(QuietHours{Start: "01:00", End: "03:00", Timezone: tc.zone}, at)
			must(t, err)
			if got != tc.want {
				t.Fatal("DST quiet-hour policy changed")
			}
		})
	}
	previous := time.Local
	t.Cleanup(func() { time.Local = previous })
	at := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	for _, zone := range []string{"America/New_York", "Asia/Kolkata"} {
		var err error
		time.Local, err = time.LoadLocation(zone)
		must(t, err)
		local, err := inQuiet(QuietHours{Start: "01:00", End: "03:00", Timezone: "Local"}, at)
		must(t, err)
		fixed, err := inQuiet(QuietHours{Start: "01:00", End: "03:00", Timezone: "America/New_York"}, at)
		must(t, err)
		if local != (zone == "America/New_York") || !fixed {
			t.Fatal("timezone change ignored or changed an explicit timezone")
		}
	}
}

func TestR4SleepingLaptopFlushesOnceAndExpiresBeforeWake(t *testing.T) {
	for _, expires := range []bool{false, true} {
		f := setup(t)
		w := watch()
		if expires {
			at := f.c.Now().Add(time.Hour)
			w.ExpiresAt = &at
		}
		_, err := f.e.CreateWatch(testCtx, w)
		must(t, err)
		must(t, f.e.OnMessage(testCtx, msg("Synthetic wake update")))
		// No ticks while asleep; only the next real-time evaluation is delivered.
		f.c.advance(8 * time.Hour)
		for i := 0; i < 3; i++ {
			must(t, f.e.FlushDue(testCtx))
		}
		if expires {
			if f.n.count("hit") != 0 || f.n.count("expired") != 1 {
				t.Fatal("expired grant ran on wake")
			}
		} else if f.n.count("hit") != 1 {
			t.Fatal("wake lost or duplicated overdue notification")
		}
	}
}

func TestR4ClockMovesBackwardThenRecovers(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("Synthetic clock update")))
	f.c.advance(-time.Hour)
	must(t, f.e.FlushDue(testCtx))
	if f.n.count("hit") != 0 {
		t.Fatal("backward clock fired future work")
	}
	f.c.advance(time.Hour + time.Minute)
	must(t, f.e.FlushDue(testCtx))
	must(t, f.e.FlushDue(testCtx))
	if f.n.count("hit") != 1 {
		t.Fatal("clock recovery lost or duplicated work")
	}
}
