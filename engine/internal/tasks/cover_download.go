package tasks

import (
	"context"
	"encoding/json"
	"limmr/internal/dbutil"
	"time"
)

// CoverMedia is the existing Live media reader, deliberately without any send API.
type CoverMedia interface {
	Connected() bool
	Download(context.Context, string, string) (string, error)
}

func (s *Service) runCoverDownloads(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.fetchRecentCovers(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Service) fetchRecentCovers(ctx context.Context) {
	s.coverMu.Lock()
	defer s.coverMu.Unlock()
	if ctx.Err() != nil || s.CoverMedia == nil || !s.CoverMedia.Connected() {
		return
	}
	cards, err := s.ListRecent(nil, "all", "", 10)
	if err != nil {
		return
	}
	for _, card := range cards {
		if ctx.Err() != nil || !s.CoverMedia.Connected() {
			return
		}
		var raw string
		var task Task
		if s.Hub.Repo.DB.QueryRow("SELECT data FROM task_cards WHERE id=?", card.ID).Scan(&raw) != nil || json.Unmarshal([]byte(raw), &task) != nil || !s.visible(nil, task) {
			continue
		}
		c := s.selectCoverCandidate(nil, task, false)
		if c == nil || c.MediaID == "" || s.coverHasFile(c) {
			continue
		}
		key := dbutil.Digest(c.ChatJID + "\x00" + c.MediaID)
		var attempted time.Time
		if s.Hub.Repo.Get("task_cover_attempts", key, &attempted) == nil && s.now().UTC().Format(time.DateOnly) == attempted.UTC().Format(time.DateOnly) {
			continue
		}
		// Persist before attempting: failures, cancellation and restarts share the daily cap.
		if s.Hub.Repo.Put("task_cover_attempts", key, s.now()) != nil {
			continue
		}
		downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = s.CoverMedia.Download(downloadCtx, c.ChatJID, c.MediaID)
		cancel()
		if err != nil || !s.coverHasFile(c) {
			continue
		}
		// Candidate URL includes this revision, invalidating positive and negative app caches.
		_, _ = s.Hub.Messages.DB.Exec("UPDATE p1_revision SET value=value+1 WHERE id=1")
	}
}
