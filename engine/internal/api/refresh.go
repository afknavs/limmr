package api

import (
	"limmr/internal/model"
	"net/http"
	"time"
)

// A tiny counter watcher covers WhatsApp/history writes that aren't AI Activity.
// Clients coalesce these hints; no message text is put on this stream.
func (s *Server) refreshStream(w http.ResponseWriter, r *http.Request) error {
	activity, done := s.Hub.Subscribe()
	defer done()
	hints := make(chan model.Activity, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(hints)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var prevH, prevM int64
		for {
			select {
			case <-stop:
				return
			case <-r.Context().Done():
				return
			case a, ok := <-activity:
				if !ok {
					return
				}
				select {
				case hints <- a:
				default:
				}
			case <-ticker.C:
				var h, m int64
				_ = s.Hub.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&h)
				_ = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&m)
				if h != prevH || m != prevM {
					prevH, prevM = h, m
					select {
					case hints <- model.Activity{Kind: "refresh"}:
					default:
					}
				}
			}
		}
	}()
	return stream(w, r, "activity", hints)
}
