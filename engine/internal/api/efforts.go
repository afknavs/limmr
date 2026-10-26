package api

import (
	"limmr/internal/efforts"
	"limmr/internal/model"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) effortRoutes(add func(string, string, route)) {
	svc := s.MCP.EffortService()
	add("GET /v1/efforts/unnamed", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, total, e := svc.Unnamed(nil, 500)
		return map[string]any{"efforts": xs, "total": total}, e
	})
	add("GET /v1/efforts", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		q := r.URL.Query()
		n := 100
		if q.Has("limit") {
			var e error
			n, e = strconv.Atoi(q.Get("limit"))
			if e != nil || n < 1 || n > 500 {
				return nil, model.ErrInvalid
			}
		}
		xs, e := svc.List(nil, efforts.Filter{Status: q.Get("status"), Query: q.Get("q"), Circle: q.Get("circle"), Person: q.Get("person"), Limit: n, IncludeUntitled: q.Get("untitled") == "true"})
		return map[string]any{"efforts": xs}, e
	})
	add("GET /v1/efforts/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		v, err := svc.Get(nil, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		xs, err := s.MCP.TaskService().List(nil, "all", 500)
		if err != nil {
			return nil, err
		}
		id := ""
		for _, t := range xs {
			if t.EffortID == v.ID {
				id = t.ID
				break
			}
		}
		return struct {
			efforts.Detail
			TaskID string `json:"task_id,omitempty"`
		}{v, id}, nil
	})
	add("PATCH /v1/efforts/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var u struct {
			Title            *string    `json:"title"`
			Summary          *string    `json:"summary"`
			OnlyUnnamed      bool       `json:"only_unnamed"`
			ExpectedLastAt   *time.Time `json:"expected_last_at"`
			ExpectedRevision string     `json:"expected_revision"`
			Status           *string    `json:"status"`
			Pinned           *bool      `json:"pinned"`
			Hidden           *bool      `json:"hidden"`
		}
		if e := decode(r, &u); e != nil {
			return nil, e
		}
		v, e := svc.Update(nil, r.PathValue("id"), efforts.Update{Title: u.Title, Summary: u.Summary, OnlyUnnamed: u.OnlyUnnamed, ExpectedLastAt: u.ExpectedLastAt, ExpectedRevision: u.ExpectedRevision, Status: u.Status, Pinned: u.Pinned, Hidden: u.Hidden})
		if e == nil {
			e = s.Hub.Record(effortActivity(v, "efforts.update", "Updated effort: "+v.Title))
		}
		return v, e
	})
	add("POST /v1/efforts/merge", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			ID      string `json:"id"`
			OtherID string `json:"other_id"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		v, e := svc.Regroup(nil, "merge", b.ID, b.OtherID, nil)
		if e == nil {
			e = s.Hub.Record(effortActivity(v, "efforts.merge", "Merged effort: "+v.Title))
		}
		return v, e
	})
	add("POST /v1/efforts/seen", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if r.ContentLength == 0 {
			return map[string]bool{"ok": true}, svc.Seen()
		}
		var b struct {
			Seen map[string]time.Time `json:"seen"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, svc.Seen(b.Seen)
	})
}
func chatIDs(e efforts.Effort) []string {
	out := []string{}
	for _, c := range e.Chats {
		out = append(out, c.ID)
	}
	return out
}

func effortActivity(v efforts.Effort, tool, note string) model.Activity {
	a := model.Activity{AgentID: "you", Tool: tool, Kind: "summary", ChatJIDs: chatIDs(v), MessageIDs: []string{}, Count: len(v.MessageRefs), Note: note}
	for _, r := range v.MessageRefs {
		a.MessageIDs = append(a.MessageIDs, r.MessageID)
	}
	return a
}
