package efforts

import (
	"context"
	"database/sql"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/wa"
	"net/url"
	"path/filepath"
	"sync"
	"time"
)

// Only immutable fields needed by Home/hints are published. The incremental
// message index and grouper remain owned by the computation lock.
type readySnapshot struct {
	scope string
	data  cached
}

func (s *Service) publish(scope string, c cached) {
	s.shared().published.Store(&readySnapshot{scope: scope, data: cached{revision: c.revision, efforts: c.efforts}})
}

var errNotReady = errors.New("efforts are warming")

// ReadyCircleUpdates never waits for an inference or starts one on the request
// goroutine. A revision miss withholds all old results, including after a scope
// or hidden-chat edit. The next refresh fills in the completed snapshot.
func (s *Service) ReadyCircleUpdates() ([]CircleUpdate, bool, error) {
	c, err := s.snapshotMode(nil, true)
	if errors.Is(err, errNotReady) {
		return []CircleUpdate{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	xs, err := s.circleUpdates(filterSnapshot(c, Filter{Limit: 500}))
	return xs, err == nil, err
}

// ReadyNamingHint counts only complete owner-visible efforts for which the
// caller can see every chat. It avoids constructing naming prompts or snippets.
func (s *Service) ReadyNamingHint(a model.Agent) (string, bool) {
	if !a.Permissions.ReadMessages || !a.Permissions.ReadPeople || !a.Permissions.SaveSummaries {
		return "", true
	}
	c, err := s.snapshotMode(nil, true)
	if err != nil {
		return "", false
	}
	visible, err := s.visible(&a)
	if err != nil {
		return "", false
	}
	total := 0
	for _, e := range c.efforts {
		if e.Hidden || goodTitle(e.Title) {
			continue
		}
		safe := true
		for _, chat := range e.Chats {
			if _, ok := visible[chat.ID]; !ok {
				safe = false
				break
			}
		}
		if safe {
			total++
		}
	}
	return NamingHint(total), true
}

// RunWarmup starts after the normal handler is installed. One worker warms the
// owner snapshot; hint counts project that snapshot with current permissions.
// TryLock lets explicit effort requests take precedence. CPU-heavy loops yield
// in bounded batches, and pause during WhatsApp history sync. Home
// and hint readers use an atomic projection, without the inference lock. The
// optional store gate protects migration; its shared lock allows normal reads.
// The caller cancels and joins this worker before closing its stores.
func (s *Service) RunWarmup(ctx context.Context, syncBusy func() bool, storeGate *sync.RWMutex) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	pause := func() {
		timer := time.NewTimer(time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for syncBusy != nil && syncBusy() && ctx.Err() == nil {
			timer.Reset(25 * time.Millisecond)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if syncBusy != nil && syncBusy() {
			continue
		}
		if storeGate != nil && !storeGate.TryRLock() {
			continue
		}
		if !s.shared().mu.TryLock() {
			if storeGate != nil {
				storeGate.RUnlock()
			}
			continue
		}
		probe := s.WithHub(s.Hub.WithContext(ctx))
		if _, err := probe.snapshotMode(nil, true); err != nil {
			s.shared().yield = pause
			_ = s.backgroundSnapshot(ctx) // Transient store errors retry next tick.
			s.shared().yield = nil
		}
		s.shared().mu.Unlock()
		if storeGate != nil {
			storeGate.RUnlock()
		}
	}
}

// The foreground store has a one-connection pool. A background history cursor
// must not monopolize that pool or queue WhatsApp writes behind tokenization.
// SQLite WAL permits this read-only connection to coexist with the writer.
// Reopen under the migration gate each pass so replaced stores are picked up.
func (s *Service) backgroundSnapshot(ctx context.Context) error {
	u := url.URL{Scheme: "file", Path: filepath.Join(s.Hub.Home, "messages.db")}
	reader, err := sql.Open("sqlite3", u.String()+"?mode=ro&_busy_timeout=1000")
	if err != nil {
		return err
	}
	defer reader.Close()
	reader.SetMaxOpenConns(1)
	h := s.Hub.WithContext(ctx)
	h.Messages = &wa.Store{DB: dbutil.WithContext(reader, ctx)}
	_, err = s.WithHub(h).snapshot(nil)
	return err
}
