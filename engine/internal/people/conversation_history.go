package people

import (
	"encoding/json"
	"limmr/internal/model"
	"strings"
	"time"
)

// ConversationHistory is an owner-facing read model. It reads metadata and
// cached thumbnails only; it never downloads media or changes message state.
type ConversationWeek struct {
	Start    string `json:"start"`
	Messages int    `json:"messages"`
}
type SharedFile struct {
	ID        string    `json:"id"`
	ChatJID   string    `json:"chat_jid"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"media_type"`
	At        time.Time `json:"at"`
	Thumbnail []byte    `json:"thumbnail,omitempty"`
}
type ConversationHistory struct {
	Weeks            []ConversationWeek `json:"weeks"`
	SharedFiles      []SharedFile       `json:"shared_files"`
	SharedFilesTotal int                `json:"shared_files_total"`
}

func (s *Service) ConversationHistory(a *model.Agent, id string) (ConversationHistory, error) {
	out := ConversationHistory{Weeks: []ConversationWeek{}, SharedFiles: []SharedFile{}}
	profile, err := s.Profile(a, id)
	if err != nil {
		return out, err
	}
	allowed := s.Hub.ChatAccess(a)
	hidden := s.Hub.HiddenChatAccess()
	jids := []string{}
	for _, jid := range profile.Person.JIDs {
		if !strings.HasSuffix(jid, "@g.us") && allowed(jid) && !hidden(jid) {
			jids = append(jids, jid)
		}
	}
	now := s.Hub.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -((int(today.Weekday())+6)%7)-77)
	for i := 0; i < 12; i++ {
		out.Weeks = append(out.Weeks, ConversationWeek{Start: start.AddDate(0, 0, i*7).Format("2006-01-02")})
	}
	if len(jids) == 0 {
		return out, nil
	}
	encoded, err := json.Marshal(jids)
	if err != nil {
		return out, err
	}
	rows, err := s.Hub.Messages.DB.Query(`SELECT CAST((julianday(timestamp)-julianday(?))/7 AS INTEGER),count(*) FROM messages WHERE chat_jid IN (SELECT value FROM json_each(?)) AND deleted_at IS NULL AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?) GROUP BY 1`, start, string(encoded), start, now)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var week, count int
		if err = rows.Scan(&week, &count); err != nil {
			rows.Close()
			return out, err
		}
		if week >= 0 && week < 12 {
			out.Weeks[week].Messages = count
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	where := `chat_jid IN (SELECT value FROM json_each(?)) AND deleted_at IS NULL AND media_type IN ('image','document','video','audio','ptt') AND julianday(timestamp)<=julianday(?)`
	if err = s.Hub.Messages.DB.QueryRow(`SELECT count(*) FROM messages WHERE `+where, string(encoded), now).Scan(&out.SharedFilesTotal); err != nil {
		return out, err
	}
	rows, err = s.Hub.Messages.DB.Query(`SELECT id,chat_jid,COALESCE(filename,''),COALESCE(media_type,''),timestamp,CASE WHEN length(preview_thumbnail)<=256000 THEN preview_thumbnail ELSE NULL END FROM messages WHERE `+where+` ORDER BY julianday(timestamp) DESC,id DESC LIMIT 20`, string(encoded), now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var file SharedFile
		if err = rows.Scan(&file.ID, &file.ChatJID, &file.Filename, &file.MediaType, &file.At, &file.Thumbnail); err != nil {
			return out, err
		}
		out.SharedFiles = append(out.SharedFiles, file)
	}
	return out, rows.Err()
}
