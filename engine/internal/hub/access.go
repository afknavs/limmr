package hub

import (
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"sort"
	"strings"
)

func (s *Service) PresentAgent(a model.Agent) model.Agent {
	a.Access = a.Permissions.Access
	a.AccessSummary = s.AccessSummary(a.Access)
	return a
}
func (s *Service) AccessSummary(v model.Access) string {
	if v.Ended(s.Now()) {
		return "Ended " + v.Until.UTC().Format("2 Jan")
	}
	text := "Everything"
	if v.Circles != nil {
		cs, _ := List[model.Circle](s.Repo, "circles")
		names := []string{}
		for _, c := range cs {
			if c.Confirmed && v.AllowsCircle(c.ID) {
				names = append(names, c.Name)
			}
		}
		sort.Strings(names)
		if v.Unsorted {
			names = append(names, "Chats in no circle")
		}
		text = strings.Join(names, ", ")
		if text == "" {
			text = "No circles"
		}
	}
	if v.Until != nil {
		text += " · until " + v.Until.UTC().Format("2 Jan")
	}
	return text
}
func (s *Service) CircleAccess(a *model.Agent, c model.Circle) bool {
	return a == nil || !a.Permissions.Access.Ended(s.Now()) && ((!c.Confirmed && c.CreatedBy == a.ID) || c.Confirmed && a.Permissions.Access.AllowsCircle(c.ID))
}

// CirclePeople snapshots membership without importing the circle/people services.
func (s *Service) CirclePeople(a *model.Agent) (map[string]bool, error) {
	if a != nil && a.RunChat != "" {
		return map[string]bool{}, nil
	}
	cs, e := List[model.Circle](s.Repo, "circles")
	out := map[string]bool{}
	if e != nil {
		return nil, e
	}
	allowed := s.ChatAccess(a)
	for _, c := range cs {
		if c.Confirmed && s.CircleAccess(a, c) && circleChatVisible(c, allowed) {
			for _, p := range c.People {
				out[p.PersonID] = true
			}
		}
	}
	return out, nil
}

// circleChatVisible matches circles.List: a circle whose chats are all outside the
// agent's chat scope is hidden, and so are the people it would reveal.
func circleChatVisible(c model.Circle, allowed func(string) bool) bool {
	if len(c.Chats) == 0 {
		return true
	}
	for _, x := range c.Chats {
		if allowed(x.ChatJID) {
			return true
		}
	}
	return false
}

// CheckAccess persists the expiry event and its deduplication marker atomically.
// Marker includes the deadline so a renewed grant can end again, even after retention.
func (s *Service) CheckAccess(a model.Agent) error {
	v := a.Permissions.Access
	if !v.Ended(s.Now()) {
		return nil
	}
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	key := a.ID + ":" + v.Until.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	res, e := tx.Exec("INSERT OR IGNORE INTO objects VALUES('access_ended',?, 'true')", key)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	var event *model.Activity
	if n > 0 {
		act := model.Activity{AgentID: a.ID, Tool: "access", Kind: "access_ended", At: s.Now().UTC(), ChatJIDs: []string{}, MessageIDs: []string{}}
		result, err := tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", act.At, dbutil.JSON(act))
		if err != nil {
			return err
		}
		act.ID, _ = result.LastInsertId()
		event = &act
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if event != nil {
		s.shared().activityMu.Lock()
		for ch := range s.shared().watchers {
			select {
			case ch <- *event:
			default:
			}
		}
		s.shared().activityMu.Unlock()
	}
	owner := s.OwnerName()
	if owner == "" {
		owner = "the owner"
	}
	return &model.AccessEndedError{Message: fmt.Sprintf("Your access to Limmr ended on %s. Ask %s to renew it in Limmr.", v.Until.UTC().Format("2 Jan 2006"), owner)}
}
func (s *Service) PatchAccess(id string, raw map[string]json.RawMessage) (model.Agent, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	a, e := s.Agent(id)
	if e != nil {
		return a, e
	}
	b, _ := json.Marshal(a.Permissions.Access)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(b, &fields)
	for k, v := range raw {
		if k != "circles" && k != "unsorted" && k != "until" {
			return a, model.ErrInvalid
		}
		fields[k] = v
	}
	// A changed circle selection gets its natural default unless explicitly supplied.
	if _, ok := raw["circles"]; ok {
		if _, ok = raw["unsorted"]; !ok {
			delete(fields, "unsorted")
		}
	}
	b, _ = json.Marshal(fields)
	if json.Unmarshal(b, &a.Permissions.Access) != nil {
		return a, model.ErrInvalid
	}
	_, e = s.Repo.DB.Exec("UPDATE objects SET data=json_set(data,'$.permissions',json(?)) WHERE bucket='agents' AND id=?", dbutil.JSON(a.Permissions), id)
	return s.PresentAgent(a), e
}
func (s *Service) EnableThisMac() (model.Agent, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	a, e := s.Agent("this-mac")
	if e != nil && e != model.ErrNotFound {
		return a, e
	}
	if e == model.ErrNotFound {
		a = model.Agent{ID: "this-mac", Name: "This Mac", Kind: "on_device", Transport: "local", CreatedAt: s.Now().UTC(), Permissions: model.Defaults(false)}
		a.Permissions.RequestSend = false
	}
	a.Enabled = true
	return s.PresentAgent(a), s.Repo.Put("agents", a.ID, a)
}
