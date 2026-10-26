package api

import (
	"limmr/internal/model"
	knowledgeSync "limmr/internal/sync"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) syncRoutes(add func(string, string, route)) {
	if s.Knowledge == nil {
		return
	}
	add("GET /v1/sync", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Knowledge.Status() })
	add("POST /v1/sync/pair", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Code string `json:"code"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if s.Remote == nil || !s.Remote.Configuration().Enabled {
			return nil, model.ErrConflict
		}
		if b.Code == "" {
			return map[string]any{"public_url": s.Remote.Configuration().PublicURL, "pending": s.Remote.Status().Pending, "instruction": "Start sync pair on your dot, then enter its six-digit code here."}, nil
		}
		e := s.Remote.Approve(b.Code, "", &struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}{"Your dot", "custom"}, nil, false, "dot")
		if e != nil {
			return nil, e
		}
		return s.Knowledge.Status()
	})
	add("DELETE /v1/sync/pair", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var peer knowledgeSync.Peer
		_ = s.Hub.Repo.Get("settings", "sync_peer", &peer)
		if peer.AgentID != "" {
			a, e := s.Hub.Agent(peer.AgentID)
			if e == nil {
				a.Permissions.Sync = false
				if e = s.Hub.UpdateAgent(a); e != nil {
					return nil, e
				}
			}
		}
		if e := s.Knowledge.Unpair(); e != nil {
			return nil, e
		}
		if e := os.Remove(filepath.Join(s.Knowledge.Home, "sync-oauth.json")); e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		return s.Knowledge.Status()
	})
	add("POST /v1/sync/lease", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Take *bool `json:"take"`
		}
		if e := decode(r, &b); e != nil || b.Take == nil {
			return nil, model.ErrInvalid
		}
		var paired knowledgeSync.Peer
		_ = s.Hub.Repo.Get("settings", "sync_peer", &paired)
		if paired.EngineID != "" {
			// While paired only the holder can act: keeping is a no-op and
			// giving hands off. A non-holder can never take, because it cannot
			// prove the holder has stopped.
			if !s.Knowledge.HoldsLease() {
				return nil, model.ErrConflict
			}
			if !*b.Take {
				if e := s.Knowledge.GiveLease(); e != nil {
					return nil, model.ErrConflict
				}
			}
			return s.Knowledge.Status()
		}
		if !*b.Take {
			return nil, model.ErrConflict
		}
		if e := s.Hub.Repo.Put("settings", "automation_lease", knowledgeSync.Lease{EngineID: s.Knowledge.EngineID, RenewedAt: s.Hub.Now()}); e != nil {
			return nil, e
		}
		return s.Knowledge.Status()
	})
	add("POST /v1/sync/pause", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Paused bool `json:"paused"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		var peer knowledgeSync.Peer
		if e := s.Hub.Repo.Get("settings", "sync_peer", &peer); e != nil {
			return nil, e
		}
		peer.Paused = b.Paused
		if e := s.Hub.Repo.Put("settings", "sync_peer", peer); e != nil {
			return nil, e
		}
		return s.Knowledge.Status()
	})
	add("POST /v1/sync/now", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if e := s.Knowledge.RunOnce(r.Context(), ""); e != nil {
			return nil, model.ErrConflict
		}
		return s.Knowledge.Status()
	})
	add("GET /v1/sync/reviews", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Knowledge.Reviews() })
	add("POST /v1/sync/reviews/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Action string `json:"action"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if e := s.Knowledge.ResolveReview(r.Context(), r.PathValue("id"), b.Action); e != nil {
			return nil, model.ErrInvalid
		}
		return s.Knowledge.Status()
	})
}
