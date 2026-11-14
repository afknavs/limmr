package tasks

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/model"
	"time"
)

type AuditTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type DuplicatePair struct {
	A AuditTask `json:"a"`
	B AuditTask `json:"b"`
}
type RulesAudit struct {
	AutoArchive []AuditTask     `json:"would_auto_archive"`
	Duplicates  []DuplicatePair `json:"possible_duplicates"`
	TaskCircles []AuditTask     `json:"circles_that_look_like_tasks"`
}

// AuditDatabase executes SELECTs only: no migrations, runtime, model or writes.
// Pass a fixture or an explicitly prepared offline copy, never the live store.
func AuditDatabase(db *sql.DB, now time.Time, loc *time.Location) (RulesAudit, error) {
	out := RulesAudit{[]AuditTask{}, []DuplicatePair{}, []AuditTask{}}
	if loc == nil {
		loc = time.Local
	}
	rows, err := db.Query("SELECT data FROM tasks ORDER BY id")
	if err != nil {
		return out, err
	}
	active := []Task{}
	for rows.Next() {
		var raw string
		var r record
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			break
		}
		if r.Purged || ended(r.Task) {
			continue
		}
		if deadlinePassed(r.Task, now, loc) {
			out.AutoArchive = append(out.AutoArchive, AuditTask{r.ID, r.Title})
		}
		active = append(active, r.Task)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	for i, a := range active {
		for _, b := range active[i+1:] {
			if duplicate(a, b) {
				out.Duplicates = append(out.Duplicates, DuplicatePair{AuditTask{a.ID, a.Title}, AuditTask{b.ID, b.Title}})
			}
		}
	}
	rows, err = db.Query("SELECT data FROM objects WHERE bucket='circles' ORDER BY id")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var c model.Circle
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return out, err
		}
		if !c.Archived && model.LooksLikeTask(c.Kind, c.Name, c.Goal) {
			out.TaskCircles = append(out.TaskCircles, AuditTask{c.ID, c.Name})
		}
	}
	return out, rows.Err()
}
