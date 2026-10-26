package hub

// U3 presentation aggregates. No model calls, network calls or message mutations.
import (
	"database/sql"
	"fmt"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
)

type UsagePeriod struct {
	Messages     int `json:"messages"`
	Sends        int `json:"sends"`
	ReadSessions int `json:"read_sessions"`
}
type AgentUsage struct {
	TodaySends int    `json:"today_sends"`
	WeekSends  int    `json:"week_sends"`
	ID         string `json:"id"`
	Today      int    `json:"today"`
	Week       int    `json:"week"`
}
type PresentedActivity struct {
	model.Activity
	Status string `json:"status,omitempty"`
}
type DashboardStats struct {
	LastSyncAt    *time.Time          `json:"last_sync_at,omitempty"`
	TrackingSince *time.Time          `json:"tracking_since,omitempty"`
	Today         UsagePeriod         `json:"today"`
	Week          UsagePeriod         `json:"week"`
	Agents        []AgentUsage        `json:"agents"`
	RecentSends   []PresentedActivity `json:"recent_sends"`
}
type ReadSession struct {
	AgentID    string
	Start, End time.Time
	Chats      []string
	Messages   int
}
type ActivityDigest struct {
	ID       string    `json:"id"`
	AgentID  string    `json:"agent_id"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Sessions int       `json:"sessions"`
	Messages int       `json:"messages"`
	Text     string    `json:"text"`
}

func messageRead(v model.Activity) bool {
	messageTool := model.Contains([]string{"list_messages", "search_messages", "get_message_context"}, v.Tool)
	// Canonical directory aliases also use read/find; only actual message IDs count.
	messageTool = messageTool || model.Contains([]string{"read", "find"}, v.Tool) && len(v.MessageIDs) > 0
	return (v.Kind == "read" || v.Kind == "search") && messageTool && v.Count > 0
}

// GroupReadSessions shares the app's 30-minute inactivity / UTC-day boundary.
func GroupReadSessions(xs []model.Activity) []ReadSession {
	xs = append([]model.Activity(nil), xs...)
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].At.Equal(xs[j].At) {
			return xs[i].ID < xs[j].ID
		}
		return xs[i].At.Before(xs[j].At)
	})
	out := []ReadSession{}
	last := map[string]int{}
	for _, v := range xs {
		if !messageRead(v) {
			continue
		}
		i, ok := last[v.AgentID]
		if !ok || v.At.Sub(out[i].End) > 30*time.Minute || v.At.UTC().Format("2006-01-02") != out[i].End.UTC().Format("2006-01-02") {
			i = len(out)
			last[v.AgentID] = i
			out = append(out, ReadSession{AgentID: v.AgentID, Start: v.At, Chats: []string{}})
		}
		out[i].End = v.At
		out[i].Messages += v.Count
		for _, chat := range v.ChatJIDs {
			if !model.Contains(out[i].Chats, chat) {
				out[i].Chats = append(out[i].Chats, chat)
			}
		}
		sort.Strings(out[i].Chats)
	}
	return out
}
func (s *Service) weekActivity(a *model.Agent) ([]model.Activity, error) {
	// Query the complete seven-day window, not the UI's first page. Apply the same
	// principal/hidden-chat rules as Activity before building any persisted text.
	allowed := s.ChatAccess(a)
	rows, e := s.Repo.DB.Query("SELECT id,data FROM activity WHERE julianday(at)>=julianday(?) AND julianday(at)<=julianday(?) ORDER BY id", s.Now().UTC().AddDate(0, 0, -7), s.Now().UTC())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Activity{}
	for rows.Next() {
		var v model.Activity
		var raw string
		var id int64
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		if e = decode(raw, &v); e != nil {
			return nil, e
		}
		v.ID = id
		if a != nil {
			if v.AgentID != a.ID {
				continue
			}
			ok := true
			for _, jid := range v.ChatJIDs {
				if !allowed(jid) {
					ok = false
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) DashboardStats() (DashboardStats, error) {
	out := DashboardStats{Agents: []AgentUsage{}, RecentSends: []PresentedActivity{}}
	now := s.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	if e := s.initSyncStats(); e != nil {
		return out, e
	}
	xs, e := s.weekActivity(nil)
	if e != nil {
		return out, e
	}
	agents, e := s.Agents()
	if e != nil {
		return out, e
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, AgentUsage{ID: a.ID})
	}
	addSend := func(p *UsagePeriod, v model.Activity) {
		p.Sends++
	}
	for _, v := range xs {
		if v.Kind == "sent" {
			for i := range out.Agents {
				if out.Agents[i].ID == v.AgentID {
					if !v.At.Before(week) {
						out.Agents[i].WeekSends++
					}
					if !v.At.Before(today) {
						out.Agents[i].TodaySends++
					}
				}
			}
			if !v.At.Before(week) {
				addSend(&out.Week, v)
			}
			if !v.At.Before(today) {
				addSend(&out.Today, v)
			}
			out.RecentSends = append(out.RecentSends, PresentedActivity{Activity: v, Status: "sent"})
		}
	}
	// Keep receipt lookup bounded to the twelve visible sends. Neither historical
	// outbound bodies nor the full archive need to be loaded for the dashboard.
	sort.Slice(out.RecentSends, func(i, j int) bool {
		if out.RecentSends[i].At.Equal(out.RecentSends[j].At) {
			return out.RecentSends[i].ID > out.RecentSends[j].ID
		}
		return out.RecentSends[i].At.After(out.RecentSends[j].At)
	})
	if len(out.RecentSends) > 12 {
		out.RecentSends = out.RecentSends[:12]
	}
	for i := range out.RecentSends {
		v := &out.RecentSends[i]
		for _, chat := range v.ChatJIDs {
			for _, id := range v.MessageIDs {
				var status sql.NullString
				e = s.Repo.DB.QueryRow(`SELECT json_extract(data,'$.status') FROM objects
     WHERE bucket IN ('drafts','approvals') AND json_extract(data,'$.chat_jid')=?
     AND json_extract(data,'$.message_id')=? ORDER BY bucket DESC LIMIT 1`, chat, id).Scan(&status)
				if e != nil && e != sql.ErrNoRows {
					return out, e
				}
				if status.Valid && status.String != "" {
					v.Status = status.String
				}
			}
		}
	}

	for _, session := range GroupReadSessions(xs) {
		if !session.Start.Before(week) {
			out.Week.ReadSessions++
		}
		if !session.Start.Before(today) {
			out.Today.ReadSessions++
		}
		for i := range out.Agents {
			if out.Agents[i].ID == session.AgentID {
				if !session.Start.Before(week) {
					out.Agents[i].Week++
				}
				if !session.Start.Before(today) {
					out.Agents[i].Today++
				}
			}
		}
	}
	if s.Messages != nil {
		e = s.Messages.DB.QueryRow("SELECT COALESCE(SUM(messages),0) FROM u3_sync_totals WHERE day>=? AND day<=?", today.Format("2006-01-02"), today.Format("2006-01-02")).Scan(&out.Today.Messages)
		if e != nil {
			return out, e
		}
		e = s.Messages.DB.QueryRow("SELECT COALESCE(SUM(messages),0) FROM u3_sync_totals WHERE day>=? AND day<=?", week.Format("2006-01-02"), today.Format("2006-01-02")).Scan(&out.Week.Messages)
		if e != nil {
			return out, e
		}
		for _, field := range []struct {
			key   string
			value **time.Time
		}{{"u3_tracking_since", &out.TrackingSince}, {"u3_last_sync_at", &out.LastSyncAt}} {
			var raw string
			e = s.Messages.DB.QueryRow("SELECT value FROM engine_meta WHERE key=?", field.key).Scan(&raw)
			if e == sql.ErrNoRows {
				continue
			}
			if e != nil {
				return out, e
			}
			at, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return out, err
			}
			*field.value = &at
		}
	}

	return out, nil
}

// ActivityDigests backfills four-hour windows on demand, including when the app
// was closed. App polling requests it every four hours. Stored owner digests are
// never returned to an agent: agent text is rebuilt with CURRENT access first.
func (s *Service) ActivityDigests(a *model.Agent) ([]ActivityDigest, error) {
	if a != nil && !a.Permissions.ReadMessages {
		return nil, model.ErrForbidden
	}
	xs, e := s.weekActivity(a)
	if e != nil {
		return nil, e
	}
	agents, e := s.Agents()
	if e != nil {
		return nil, e
	}
	names := map[string]string{}
	for _, agent := range agents {
		names[agent.ID] = agent.Name
	}
	chats, e := s.Messages.Chats()
	if e != nil {
		return nil, e
	}
	chatNames := map[string]string{}
	for _, c := range chats {
		chatNames[c.JID] = c.Name
	}
	buckets := map[string][]model.Activity{}
	starts := map[string]time.Time{}
	for _, v := range xs {
		if !messageRead(v) {
			continue
		}
		start := v.At.UTC().Truncate(4 * time.Hour)
		key := v.AgentID + "/" + start.Format(time.RFC3339)
		buckets[key] = append(buckets[key], v)
		starts[key] = start
	}
	out := []ActivityDigest{}
	for key, events := range buckets {
		sessions := GroupReadSessions(events)
		count := 0
		scopes := []string{}
		for _, session := range sessions {
			count += session.Messages
			for _, chat := range session.Chats {
				name := chatNames[chat]
				if name == "" {
					name = "a chat"
				}
				if !model.Contains(scopes, name) {
					scopes = append(scopes, name)
				}
			}
		}
		sort.Strings(scopes)
		scope := "messages"
		if len(scopes) > 0 {
			scope = strings.Join(scopes, " and ")
		}
		agent := names[events[0].AgentID]
		if agent == "" {
			agent = "Integration"
		}
		messageWord, sessionWord := "messages", "read sessions"
		if count == 1 {
			messageWord = "message"
		}
		if len(sessions) == 1 {
			sessionWord = "read session"
		}
		d := ActivityDigest{ID: key, AgentID: events[0].AgentID, Start: starts[key], End: starts[key].Add(4 * time.Hour), Sessions: len(sessions), Messages: count, Text: fmt.Sprintf("%s caught up on %s · %d %s across %d %s.", agent, scope, count, messageWord, len(sessions), sessionWord)}
		// Owner and per-agent caches are separate; rebuilding prevents stale scope leaks.
		bucket := "activity_digests"
		if a != nil {
			bucket = "activity_digests_agent"
		}
		var old ActivityDigest
		if s.Repo.Get(bucket, key, &old) != nil || old != d {
			if e = s.Repo.Put(bucket, key, d); e != nil {
				return nil, e
			}
		}
		out = append(out, d)
	}
	// Match activity retention and keep this derived cache bounded to one week.
	if _, e = s.Repo.DB.Exec("DELETE FROM objects WHERE bucket IN ('activity_digests','activity_digests_agent') AND julianday(json_extract(data,'$.end'))<julianday(?)", s.Now().UTC().AddDate(0, 0, -7)); e != nil {
		return nil, e
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].ID < out[j].ID
		}
		return out[i].Start.After(out[j].Start)
	})
	return out, nil
}

// Ingestion counters start at installation; no historical sync dates are guessed.
// INSERT counts new stored messages once; upserts refresh the observed sync time.
// This adds no message text and never sends acknowledgements or read receipts.
func (s *Service) initSyncStats() error {
	if _, e := s.Repo.DB.Exec(`CREATE INDEX IF NOT EXISTS u3_activity_instant ON activity(julianday(at),id);
 CREATE INDEX IF NOT EXISTS u3_outbound_receipts ON objects(json_extract(data,'$.chat_jid'),json_extract(data,'$.message_id'),bucket) WHERE bucket IN ('drafts','approvals');`); e != nil {
		return e
	}
	if s.Messages == nil {
		return nil
	}
	_, e := s.Messages.DB.Exec(`
 CREATE TABLE IF NOT EXISTS u3_sync_totals(day TEXT PRIMARY KEY,messages INTEGER NOT NULL);
 INSERT OR IGNORE INTO engine_meta(key,value) VALUES('u3_tracking_since',strftime('%Y-%m-%dT%H:%M:%fZ','now'));
 CREATE TRIGGER IF NOT EXISTS u3_synced_insert AFTER INSERT ON messages BEGIN
   INSERT INTO u3_sync_totals(day,messages) VALUES(date('now'),1) ON CONFLICT(day) DO UPDATE SET messages=messages+1;
   INSERT INTO engine_meta(key,value) VALUES('u3_last_sync_at',strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(key) DO UPDATE SET value=excluded.value;
 END;
 CREATE TRIGGER IF NOT EXISTS u3_synced_update AFTER UPDATE ON messages BEGIN
   INSERT INTO engine_meta(key,value) VALUES('u3_last_sync_at',strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(key) DO UPDATE SET value=excluded.value;
 END;`)
	return e
}
