package remote

import (
	knowledgeSync "limmr/internal/sync"
	"net/http"
	"strings"
)

// SyncHandler uses the existing OAuth credential and the live owner-granted
// capability, independently of MCP read/write/send permissions.
func (s *Service) SyncHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Knowledge == nil {
			http.NotFound(w, r)
			return
		}
		headers := r.Header.Values("Authorization")
		if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
			fail(w, "invalid_token", 401)
			return
		}
		ti, e := s.Verify(r.Context(), strings.TrimPrefix(headers[0], "Bearer "), r)
		if e != nil {
			fail(w, "invalid_token", 401)
			return
		}
		a, e := s.Hub.ActiveAgent(ti.UserID)
		if e != nil || !a.Permissions.Sync {
			fail(w, "forbidden", 403)
			return
		}
		var p knowledgeSync.Peer
		if s.Hub.Repo.Get("settings", "sync_peer", &p) != nil || p.EngineID == "" || p.AgentID != a.ID || p.Paused {
			fail(w, "sync_unavailable", 403)
			return
		}
		s.Knowledge.Handler().ServeHTTP(w, r)
	})
}
