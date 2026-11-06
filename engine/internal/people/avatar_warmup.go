package people

import (
	"context"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"sync"
	"time"
)

// WarmNext attempts at most one eligible missing photo, newest conversation first.
// Get owns the persisted two-second limiter and seven-day miss rule, shared with
// foreground requests. This interface has no message/presence/send operation.
func (s *AvatarCache) WarmNext(ctx context.Context, connected func() bool) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if connected == nil || !connected() || s.Lookup == nil {
		return false, nil
	}
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return false, err
	}
	gs, err := hub.List[model.Group](s.Hub.Repo, "groups")
	if err != nil {
		return false, err
	}
	chats, err := s.Hub.Messages.Chats()
	if err != nil {
		return false, err
	}
	recent := map[string]time.Time{}
	for _, chat := range chats {
		if chat.LastMessageTime != nil {
			recent[chat.JID] = *chat.LastMessageTime
		}
	}
	type candidate struct {
		id string
		at time.Time
	}
	candidates := []candidate{}
	for _, p := range ps {
		hasPhoto := false
		for _, ref := range []*model.PhotoReference{p.Photo, p.ContactsPhoto} {
			if ref != nil {
				if profilePhotoExists(s.Hub.Home, ref.SHA256) {
					hasPhoto = true
					break
				}
			}
		}
		if hasPhoto {
			continue
		}
		at := time.Time{}
		valid := false
		for _, jid := range p.JIDs {
			if strings.HasSuffix(jid, "@s.whatsapp.net") || strings.HasSuffix(jid, "@lid") {
				valid = true
				if recent[jid].After(at) {
					at = recent[jid]
				}
			}
		}
		if valid {
			candidates = append(candidates, candidate{p.ID, at})
		}
	}
	for _, g := range gs {
		candidates = append(candidates, candidate{g.JID, recent[g.JID]})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].at.Equal(candidates[j].at) {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].at.After(candidates[j].at)
	})
	for _, c := range candidates {
		var entry avatarEntry
		err := s.Hub.Repo.Get("avatar_cache", c.id, &entry)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return false, err
		}
		// Full photos stay warm; legacy previews get one immediate upgrade.
		if entry.Available && entry.Full || !entry.due(s.Hub.Now()) {
			continue
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if !connected() {
			return false, nil
		}
		attempted := false
		_, err = s.get(ctx, c.id, &attempted)
		if errors.Is(err, model.ErrNotFound) {
			return attempted, nil
		}
		return attempted, err
	}
	return false, nil
}

// StartWarmup is idempotent per cache and cancellation joins through its done
// channel. The engine's storage gate excludes migration/reset while a tick runs.
func (s *AvatarCache) StartWarmup(ctx context.Context, connected func() bool, gate *sync.RWMutex) <-chan struct{} {
	s.warmOnce.Do(func() {
		s.warmDone = make(chan struct{})
		go func() {
			defer close(s.warmDone)
			s.runWarmup(ctx, connected, gate, waitForAvatarPass)
		}()
	})
	return s.warmDone
}

func waitForAvatarPass(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *AvatarCache) runWarmup(ctx context.Context, connected func() bool, gate *sync.RWMutex, wait func(context.Context, time.Duration) bool) {
	for ctx.Err() == nil {
		if gate != nil {
			gate.RLock()
		}
		attempted, _ := s.WarmNext(ctx, connected)
		if gate != nil {
			gate.RUnlock()
		}
		delay := 5 * time.Minute
		if attempted {
			delay = 2 * time.Second
		}
		if !wait(ctx, delay) {
			return
		}
	}
}
