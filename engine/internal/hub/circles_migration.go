package hub

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
)

// migrateCircles is atomic and restart-safe. Circle/brief/cursor/topic records use
// the existing transactional object store; schema version 2 indexes their keys.
func (r *Repo) migrateCircles() error {
	tx, e := r.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS hub_schema(version INTEGER PRIMARY KEY);
 CREATE INDEX IF NOT EXISTS circle_confirmed ON objects(bucket,json_extract(data,'$.confirmed')) WHERE bucket='circles';
 CREATE UNIQUE INDEX IF NOT EXISTS brief_version ON objects(json_extract(data,'$.circle_id'),json_extract(data,'$.version')) WHERE bucket='briefs';`); e != nil {
		return e
	}
	var n int
	if e = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=2").Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return tx.Commit()
	}
	rows, e := tx.Query("SELECT data FROM objects WHERE bucket='approvals' AND json_extract(data,'$.state')='pending'")
	if e != nil {
		return e
	}
	// Keep the legacy metadata only until migration 7 repairs pending previews.
	type legacyApproval struct {
		model.Approval
		Voice string `json:"voice,omitempty"`
	}
	xs := []legacyApproval{}
	for rows.Next() {
		var raw string
		var a legacyApproval
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			rows.Close()
			return e
		}
		xs = append(xs, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, a := range xs {
		d := model.Draft{ID: a.ID, AgentID: a.AgentID, ChatJID: a.ChatJID, Kind: a.Kind, FinalText: a.Text, ReplyTo: a.ReplyTo, Attachment: a.Attachment, Rule: "confirm", NeedsConfirmation: true, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, State: "suggested", SuggestionID: "legacy-" + a.ID}
		x := model.Suggestion{ID: d.SuggestionID, Kind: "reply", ChatJID: a.ChatJID, DraftText: a.Text, Why: "Parked draft from the previous approval queue.", CreatedBy: a.AgentID, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, State: "pending", DraftID: a.ID}
		for _, v := range []struct {
			bucket, id string
			value      any
		}{{"drafts", d.ID, d}, {"suggestions", x.ID, x}} {
			if _, e = tx.Exec("INSERT OR IGNORE INTO objects VALUES(?,?,?)", v.bucket, v.id, func() string {
				var value map[string]any
				_ = json.Unmarshal([]byte(dbutil.JSON(v.value)), &value)
				value["voice"] = a.Voice
				return dbutil.JSON(value)
			}()); e != nil {
				return e
			}
		}
		a.State = "suggested"
		if _, e = tx.Exec("UPDATE objects SET data=? WHERE bucket='approvals' AND id=?", dbutil.JSON(a), a.ID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("INSERT INTO hub_schema VALUES(2)"); e != nil {
		return e
	}
	return tx.Commit()
}
