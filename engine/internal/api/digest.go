package api

import (
	"limmr/internal/hub"
	"net/http"
)

func (s *Server) digestRoute(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
	return s.Hub.GetDigest(p.agent, hub.DigestOptions{Period: r.URL.Query().Get("period"), CircleID: r.URL.Query().Get("circle_id"), Cursor: r.URL.Query().Get("cursor")}, s.MCP.CircleService())
}
