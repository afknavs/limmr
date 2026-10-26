package hub

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"time"
)

// TaskSurfacingChange couples an archived circle (if any) to a task for owner Undo.
type TaskSurfacingChange struct {
	ID     string        `json:"id"`
	Kind   string        `json:"kind"`
	TaskID string        `json:"task_id"`
	Before *model.Circle `json:"before,omitempty"`
	After  *model.Circle `json:"after,omitempty"`
	At     time.Time     `json:"at"`
}

func (r *Repo) migrateTaskSurfacing() error { return r.MigrateTaskSurfacing(time.Now().UTC()) }

// MigrateTaskSurfacing is version 10. All conversions and the marker commit together.
func (r *Repo) MigrateTaskSurfacing(now time.Time) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=10").Scan(&n); err != nil || n > 0 {
		return err
	}
	if _, err = tx.Exec(`UPDATE tasks SET data=json_set(data,'$.origin',CASE WHEN json_extract(data,'$.created_by.agent_id')='you' THEN 'owner' ELSE 'agent' END) WHERE json_extract(data,'$.origin') IS NULL;`); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT data FROM objects WHERE bucket='circles' AND json_extract(data,'$.confirmed')=1 AND COALESCE(json_extract(data,'$.archived'),0)=0 AND json_extract(data,'$.kind')='project' AND json_array_length(data,'$.chats')=1")
	if err != nil {
		return err
	}
	cs := []model.Circle{}
	for rows.Next() {
		var raw string
		var c model.Circle
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal([]byte(raw), &c)
		}
		if err != nil {
			rows.Close()
			return err
		}
		cs = append(cs, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range cs {
		before := c
		c.Archived = true
		c.UpdatedAt = now
		id := "tsk_" + dbutil.ID()
		changeID := "chg_" + dbutil.ID()
		name := "Former AI"
		var raw string
		if e := tx.QueryRow("SELECT data FROM objects WHERE bucket='agents' AND id=?", c.CreatedBy).Scan(&raw); e == nil {
			var a model.Agent
			if err = json.Unmarshal([]byte(raw), &a); err != nil {
				return err
			}
			name = a.Name
		}
		chats := []map[string]string{}
		people := []map[string]string{}
		for _, ch := range c.Chats {
			chats = append(chats, map[string]string{"id": ch.ChatJID, "name": ""})
		}
		for _, p := range c.People {
			name := ""
			if e := tx.QueryRow("SELECT data FROM objects WHERE bucket='people' AND id=?", p.PersonID).Scan(&raw); e == nil {
				var person model.Person
				if err = json.Unmarshal([]byte(raw), &person); err != nil {
					return err
				}
				name = person.Name
			}
			people = append(people, map[string]string{"id": p.PersonID, "name": name})
		}
		summary := model.Brief{}
		if e := tx.QueryRow("SELECT data FROM objects WHERE bucket='briefs' AND json_extract(data,'$.circle_id')=? ORDER BY json_extract(data,'$.version') DESC LIMIT 1", c.ID).Scan(&raw); e == nil {
			if err = json.Unmarshal([]byte(raw), &summary); err != nil {
				return err
			}
		}
		title := []rune(c.Name)
		if len(title) > 60 {
			title = title[:60]
		}
		text := []rune(summary.Summary)
		if len(text) > 1500 {
			text = text[:1500]
		}
		task := map[string]any{"id": id, "title": string(title), "goal": "", "kind": "long", "status": "active", "origin": "agent", "chats": chats, "people": people, "circle_ids": []string{}, "created_by": map[string]string{"agent_id": c.CreatedBy, "name": name}, "created_at": now, "updated_at": now, "summary": map[string]any{"text": string(text), "updated_at": summary.WrittenAt, "updated_by": summary.WrittenBy}, "items": []any{}, "checklist": []any{}, "progress": map[string]int{"done": 0, "total": 0}, "updates_sent": []any{}, "needs_you": false, "lifespan_days": 14, "auto_kept_at": now, "undo_change_id": changeID}
		line := "Moved '" + c.Name + "' from circles to tasks"
		change := TaskSurfacingChange{ID: changeID, Kind: "circle_to_task", TaskID: id, Before: &before, After: &c, At: now}
		if _, err = tx.Exec("INSERT INTO tasks VALUES(?,?)", id, dbutil.JSON(task)); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='circles' AND id=?", dbutil.JSON(c), c.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO objects VALUES('changes',?,?)", changeID, dbutil.JSON(change)); err != nil {
			return err
		}
		eventID := id + ":migration"
		if _, err = tx.Exec("INSERT INTO task_events VALUES(?,?,?,?,?,?)", eventID, id, now.Format(time.RFC3339Nano), "task_suggested", line, dbutil.JSON(map[string]any{"text": line})); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO agent_updates(agent,at,kind,ref) SELECT id,?,'task_suggested',? FROM objects WHERE bucket='agents' AND json_extract(data,'$.preset')='own'", now.Format(time.RFC3339Nano), eventID); err != nil {
			return err
		}
		act := model.Activity{At: now, AgentID: c.CreatedBy, Kind: "people_write", Tool: "circle_to_task", MessageIDs: []string{}, ChatJIDs: []string{c.Chats[0].ChatJID}, Count: 1, Note: line}
		if _, err = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", now, dbutil.JSON(act)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO hub_schema VALUES(10)"); err != nil {
		return err
	}
	return tx.Commit()
}
