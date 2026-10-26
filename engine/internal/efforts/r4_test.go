package efforts

import (
	"limmr/internal/model"
	"testing"
	"time"
)

func TestR4DeadlineCountdownAcrossDSTAndTravel(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		month time.Month
		day   int
		hours time.Duration
	}{
		{time.March, 7, 23}, {time.October, 31, 25},
	} {
		at := time.Date(2026, tc.month, tc.day, 12, 0, 0, 0, loc)
		deadline := messageDue(model.Message{Content: "Pitch deck due tomorrow at 12pm", Timestamp: at})
		if deadline == nil || deadline.Hour() != 12 || deadline.Sub(at) != tc.hours*time.Hour {
			t.Fatal("relative deadline used 24h instead of the calendar across DST")
		}
		// Countdown instants do not change when the laptop travels or wakes.
		for _, zone := range []string{"UTC", "Asia/Kolkata", "America/New_York"} {
			there, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			now := at.In(there)
			if deadline.Sub(now) != tc.hours*time.Hour || !dueSoon(deadline, now) {
				t.Fatal("timezone changed absolute countdown")
			}
			if dueSoon(deadline, now.Add(26*time.Hour)) {
				t.Fatal("expired deadline stayed upcoming after sleep")
			}
		}
	}
}
