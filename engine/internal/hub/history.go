package hub

import (
	"limmr/internal/model"
	"time"
)

// DailyHistory counts stored messages by their original UTC timestamp, never by ingestion time.
// AgentReads is the number of message-read calls (including searches), not unique messages.
type DailyHistory struct {
	Date        string                  `json:"date"`
	MessagesIn  int                     `json:"messages_in"`
	MessagesOut int                     `json:"messages_out"`
	AgentSends  int                     `json:"agent_sends"`
	AgentReads  int                     `json:"agent_reads"`
	Agents      map[string]HistoryAgent `json:"agents"`
}
type HistoryAgent struct {
	Reads int `json:"reads"`
	Sends int `json:"sends"`
}
type StatsHistory struct {
	Days []DailyHistory `json:"days"`
}

func (s *Service) StatsHistory(days int) (StatsHistory, error) {
	out := StatsHistory{Days: []DailyHistory{}}
	if days < 1 || days > 366 {
		return out, model.ErrInvalid
	}
	now := s.Now().UTC()
	start := now.Truncate(24*time.Hour).AddDate(0, 0, 1-days)
	indices := map[string]int{}
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		indices[day] = i
		out.Days = append(out.Days, DailyHistory{Date: day, Agents: map[string]HistoryAgent{}})
	}
	if s.Messages != nil {
		rows, e := s.Messages.DB.Query(`SELECT date(timestamp), SUM(CASE WHEN is_from_me=1 THEN 0 ELSE 1 END), SUM(CASE WHEN is_from_me=1 THEN 1 ELSE 0 END) FROM messages WHERE julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?) GROUP BY date(timestamp)`, start, now)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var day string
			var in, outbound int
			if e = rows.Scan(&day, &in, &outbound); e != nil {
				rows.Close()
				return out, e
			}
			if i, ok := indices[day]; ok {
				out.Days[i].MessagesIn = in
				out.Days[i].MessagesOut = outbound
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	rows, e := s.Repo.DB.Query(`SELECT date(at), COALESCE(json_extract(data,'$.agent_id'),''), SUM(CASE WHEN json_extract(data,'$.kind')='sent' THEN 1 ELSE 0 END), SUM(CASE WHEN json_extract(data,'$.kind') IN ('read','search') AND (json_extract(data,'$.tool') IN ('list_messages','search_messages','get_message_context') OR (json_extract(data,'$.tool') IN ('read','find') AND json_array_length(json_extract(data,'$.message_ids'))>0)) THEN 1 ELSE 0 END) FROM activity WHERE julianday(at)>=julianday(?) AND julianday(at)<=julianday(?) GROUP BY date(at),json_extract(data,'$.agent_id')`, start, now)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var day, id string
		var a HistoryAgent
		if e = rows.Scan(&day, &id, &a.Sends, &a.Reads); e != nil {
			return out, e
		}
		if i, ok := indices[day]; ok {
			out.Days[i].AgentSends += a.Sends
			out.Days[i].AgentReads += a.Reads
			out.Days[i].Agents[id] = a
		}
	}
	return out, rows.Err()
}

// Keep the legacy /stats shape, while correcting its message totals for old clients too.
func (s *Service) HistoricalDashboardStats() (DashboardStats, error) {
	v, e := s.DashboardStats()
	if e != nil {
		return v, e
	}
	h, e := s.StatsHistory(7)
	if e != nil {
		return v, e
	}
	v.Today.Messages = 0
	v.Week.Messages = 0
	monday := s.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(int(s.Now().UTC().Weekday())+6)%7).Format("2006-01-02")
	for i, d := range h.Days {
		if i == len(h.Days)-1 {
			v.Today.Messages = d.MessagesIn + d.MessagesOut
		}
		if d.Date >= monday {
			v.Week.Messages += d.MessagesIn + d.MessagesOut
		}
	}
	return v, nil
}
