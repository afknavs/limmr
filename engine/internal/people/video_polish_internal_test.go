package people

import (
	"context"
	"testing"
	"time"
)

func TestVideoIdleWarmupSchedulesFiveMinutesAndCancelsPromptly(t *testing.T) {
	cache := &AvatarCache{}
	waits := 0
	cache.runWarmup(t.Context(), func() bool { return false }, nil, func(_ context.Context, d time.Duration) bool {
		waits++
		if d != 5*time.Minute {
			t.Fatal("idle delay", d)
		}
		return false
	})
	if waits != 1 {
		t.Fatal("did not schedule idle wait")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan bool, 1)
	go func() { done <- waitForAvatarPass(ctx, 5*time.Minute) }()
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("cancelled wait reported timer")
		}
	case <-time.After(time.Second):
		t.Fatal("idle wait did not cancel")
	}
}
