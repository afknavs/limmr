package api

import "net/http"

func (s *Server) statsRoutes(add func(string, string, route)) {
	add("GET /v1/stats", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		stats, err := s.Hub.HistoricalDashboardStats()
		// Database upserts also happen during history imports and receipt updates.
		// Home reports receipt of a WhatsApp message event, independently of storage.
		stats.LastSyncAt = nil
		if s.Link != nil {
			_, _, stats.LastSyncAt = s.Link.Connection()
		}
		return stats, err
	})
	add("GET /v1/activity/digests", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.Hub.ActivityDigests(p.agent)
		return map[string]any{"digests": xs}, e
	})
}
