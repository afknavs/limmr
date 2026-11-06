package tasks

import (
	"encoding/json"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/url"
	"sort"
	"strings"
	"time"
)

type recentEvent struct {
	at                   time.Time
	task, kind, text, by string
}

// Recently uses only stored event labels and attributed audit actions. It never
// copies message/note bodies, and filters before counting or choosing a window.
// circle optionally narrows a brief to its own workspace.
type RecentLine struct {
	Text     string    `json:"text"`
	TargetID string    `json:"target_id"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
	By       string    `json:"by"`
}

func (s *Service) Recently(a *model.Agent, circle string) ([]string, error) {
	return s.recently(a, circle, nil)
}

// RecentlyWithTargets preserves the MCP strings and adds safe navigation anchors for the owner UI.
func (s *Service) RecentlyWithTargets(a *model.Agent, circle string) ([]string, []RecentLine, error) {
	lines := []RecentLine{}
	text, err := s.recently(a, circle, &lines)
	return text, lines, err
}
func (s *Service) recently(a *model.Agent, circle string, lines *[]RecentLine) ([]string, error) {
	if a != nil && (!a.Permissions.ReadMessages || s.Hub.CheckAccess(*a) != nil) {
		return []string{}, nil
	}
	cards, err := s.recordsForRecently(a, circle)
	if err != nil {
		return nil, err
	}
	agents, err := hub.List[model.Agent](s.Hub.Repo, "agents")
	if err != nil {
		return nil, err
	}
	names := map[string]string{"you": "You", "This Mac": "This Mac"}
	for _, agent := range agents {
		names[agent.ID] = agent.Name
	}
	cutoff := s.now().Add(-72 * time.Hour)
	rows, err := s.Hub.Repo.DB.Query("SELECT task_id,at,kind,text,payload FROM task_events WHERE julianday(at)>=julianday(?) AND julianday(at)<=julianday(?) ORDER BY julianday(at) DESC,id", cutoff, s.now())
	if err != nil {
		return nil, err
	}
	events := []recentEvent{}
	allowed := map[string]bool{"created": true, "status_changed": true, "item_added": true, "step_added": true, "step_done": true, "step_reopened": true, "task_owner_note": true, "task_due": true, "check_in_skipped": true, "check_in_received": true, "update_sent": true, "task_closed": true, "task_waiting_cleared": true, "waiting_set": true, "kept": true, "summary_changed": true, "circles_changed": true, "reopened": true}
	for rows.Next() {
		var task, at, kind, label, payload string
		if err = rows.Scan(&task, &at, &kind, &label, &payload); err != nil {
			break
		}
		v, ok := cards[task]
		if !ok || !allowed[kind] {
			continue
		}
		instant, e := time.Parse(time.RFC3339Nano, at)
		if e != nil {
			continue
		}
		var p struct {
			By string `json:"by"`
		}
		_ = json.Unmarshal([]byte(payload), &p)
		by := names[p.By]
		if kind == "created" {
			by = v.CreatedBy.Name
		}
		events = append(events, recentEvent{at: instant, task: task, kind: kind, text: recentTaskText(v.Title, label, by), by: by})
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// General AI actions have no safe task anchor. Require concrete visible chat
	// provenance; a brief additionally requires every chat to be in that circle.
	circleChats := map[string]bool{}
	if circle != "" {
		var c model.Circle
		if err := s.Hub.Repo.Get("circles", circle, &c); err != nil || !s.Hub.CircleAccess(a, c) {
			return []string{}, nil
		}
		for _, ch := range c.Chats {
			circleChats[ch.ChatJID] = true
		}
	}
	allowedChats := s.Hub.ChatAccess(a)
	hiddenChat := s.Hub.HiddenChatAccess()
	rows, err = s.Hub.Repo.DB.Query("SELECT data FROM activity WHERE julianday(at)>=julianday(?) AND julianday(at)<=julianday(?) ORDER BY id DESC", cutoff, s.now())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var act model.Activity
		if json.Unmarshal([]byte(raw), &act) != nil || act.Kind != "summary" && act.Kind != "people_write" || act.Tool == "task" || strings.HasPrefix(act.Tool, "task_") || len(act.ChatJIDs) == 0 {
			continue
		}
		visible := true
		for _, chat := range act.ChatJIDs {
			if hiddenChat(chat) || !allowedChats(chat) || circle != "" && !circleChats[chat] {
				visible = false
			}
		}
		if !visible {
			continue
		}
		by := names[act.AgentID]
		if by == "" {
			by = "An AI"
		}
		label := "saved a summary"
		if act.Kind == "people_write" {
			label = "updated shared context"
		}
		events = append(events, recentEvent{at: act.At, kind: "ai_action", text: by + " " + label + ".", by: by})
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.After(events[j].at) })
	window := "the last 24 hours"
	recent := []recentEvent{}
	for _, e := range events {
		if !e.at.Before(s.now().Add(-24 * time.Hour)) {
			recent = append(recent, e)
		}
	}
	meaningful := func(rows []recentEvent) bool {
		for _, e := range rows {
			if e.kind != "item_added" {
				return true
			}
		}
		return false
	}
	if len(recent) == 0 || !meaningful(recent) && meaningful(events) {
		recent = events
		window = "the last 3 days"
	}
	// Prefer actual progress over workspace housekeeping, even when additions are newer.
	sort.SliceStable(recent, func(i, j int) bool {
		if (recent[i].kind == "item_added") != (recent[j].kind == "item_added") {
			return recent[i].kind != "item_added"
		}
		return recent[i].at.After(recent[j].at)
	})
	folded := []recentEvent{}
	additions := map[string][]recentEvent{}
	for _, e := range recent {
		if e.kind == "item_added" {
			additions[e.task] = append(additions[e.task], e)
		}
	}
	for _, e := range recent {
		if e.kind == "item_added" {
			group, exists := additions[e.task]
			if !exists {
				continue
			}
			delete(additions, e.task)
			label, err := s.recentItemLabel(a, e.task, group)
			if err != nil {
				return nil, err
			}
			// Several unknown/mixed actors must never acquire a made-up attribution.
			for _, other := range group {
				if other.by != e.by {
					e.by = ""
				}
			}
			e.text = recentTaskText(cards[e.task].Title, label, e.by)
		}
		folded = append(folded, e)
	}
	recent = folded
	moved := map[string]bool{}
	notes, checks, ai := 0, 0, 0
	for _, e := range recent {
		if e.task != "" {
			moved[e.task] = true
		}
		if e.kind == "task_owner_note" {
			notes++
		}
		if e.kind == "task_due" || e.kind == "check_in_received" {
			checks++
		}
		if e.kind == "ai_action" {
			ai++
		}
	}
	out := []string{fmt.Sprintf("%s changed in %s.", recentCount(len(moved), "task", "tasks"), window), fmt.Sprintf("%s · %s · %s.", recentCount(notes, "note update", "note updates"), recentCount(checks, "check-in", "check-ins"), recentCount(ai, "other AI action", "other AI actions"))}
	seen := map[string]bool{}
	for _, e := range recent {
		if !seen[e.text] {
			out = append(out, e.text)
			seen[e.text] = true
		}
		if len(out) == 5 {
			break
		}
	}
	if len(out) == 2 {
		out = append(out, "No further activity in your available work.")
	}
	if lines != nil {
		seenTasks := map[string]bool{}
		for _, e := range recent {
			if e.task == "" || seenTasks[e.task] {
				continue
			}
			*lines = append(*lines, RecentLine{Text: e.text, TargetID: e.task, Kind: "task", At: e.at, By: e.by})
			seenTasks[e.task] = true
			if len(*lines) == 3 {
				break
			}
		}
	}
	return out, nil
}
func (s *Service) recordsForRecently(a *model.Agent, circle string) (map[string]Task, error) {
	rows, err := s.Hub.Repo.DB.Query("SELECT data FROM task_cards")
	if err != nil {
		return nil, err
	}
	all := []Task{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var v Task
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			break
		}
		all = append(all, v)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[string]Task{}
	for _, v := range all {
		if (circle == "" || model.Contains(v.CircleIDs, circle)) && s.visible(a, v) {
			out[v.ID] = v
		}
	}
	return out, nil
}

func recentCount(n int, singular, plural string) string {
	word := plural
	if n == 1 {
		word = singular
	}
	return fmt.Sprintf("%d %s", n, word)
}

func recentTaskText(title, label, by string) string {
	prefix := ""
	if by != "" {
		prefix = by + " · "
	}
	return prefix + title + ": " + strings.TrimRight(label, ".") + "."
}

// Project only safe metadata from the workspace; never load text, captions, OCR or note bodies.
func (s *Service) recentItemLabel(a *model.Agent, task string, events []recentEvent) (string, error) {
	allowedChat, hiddenChat := s.Hub.ChatAccess(a), s.Hub.HiddenChatAccess()
	rows, err := s.Hub.Repo.DB.Query(`SELECT coalesce(json_extract(j.value,'$.kind'),''),coalesce(json_extract(j.value,'$.title'),''),coalesce(json_extract(j.value,'$.url'),''),coalesce(json_extract(j.value,'$.added_at'),''),coalesce(json_extract(j.value,'$.message_ref.chat_jid'),'') FROM tasks t,json_each(t.data,'$.items') j WHERE t.id=?`, task)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	counts := map[string]int{}
	labels := []string{}
	for rows.Next() {
		var kind, title, rawURL, added, chat string
		if err := rows.Scan(&kind, &title, &rawURL, &added, &chat); err != nil {
			return "", err
		}
		if chat != "" && (hiddenChat(chat) || !allowedChat(chat)) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, added)
		if err != nil {
			continue
		}
		matches := false
		for _, e := range events {
			if at.Sub(e.at).Abs() < time.Second {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		switch kind {
		case "file":
			counts["files"]++
			if title != "" {
				labels = append(labels, "Added a file: "+title)
			} else {
				labels = append(labels, "Added a file")
			}
		case "link":
			counts["links"]++
			name := title
			if parsed, err := url.Parse(rawURL); err == nil && parsed.Hostname() != "" {
				name = parsed.Hostname()
			}
			if name != "" {
				labels = append(labels, "Added a link: "+name)
			} else {
				labels = append(labels, "Added a link")
			}
		default:
			counts["items"]++
			labels = append(labels, "Added a workspace item")
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(labels) == 1 {
		return labels[0], nil
	}
	if len(labels) > 1 {
		parts := []string{}
		for _, kind := range []string{"files", "links", "items"} {
			if n := counts[kind]; n > 0 {
				word := kind
				if n == 1 {
					word = strings.TrimSuffix(kind, "s")
				}
				parts = append(parts, fmt.Sprintf("%d %s", n, word))
			}
		}
		return "Added " + strings.Join(parts, " and "), nil
	}
	return "Added workspace items", nil
}
