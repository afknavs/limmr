package hub

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
)

// Version 5 deliberately does not reuse CirclePerson.Source: earlier owner edits
// could retain the previous writer's source and had no role timestamp.
func (r *Repo) migrateUnderstanding() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=5").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}
	rows, err := tx.Query("SELECT data FROM objects WHERE bucket='circles'")
	if err != nil {
		return err
	}
	cs := []model.Circle{}
	for rows.Next() {
		var raw string
		var c model.Circle
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
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
		unknown := model.Origin{By: "Structure", ByKind: "structure", At: c.UpdatedAt}
		if c.Origin == nil {
			c.Origin = &unknown
		}
		if c.Confirmed && c.ConfirmOrigin == nil {
			c.ConfirmOrigin = &unknown
		}
		for i := range c.Chats {
			if c.Chats[i].Origin == nil {
				c.Chats[i].Origin = &unknown
			}
		}
		for i := range c.People {
			if c.People[i].Origin == nil {
				c.People[i].Origin = &unknown
			}
		}
		for i := range c.Topics {
			if c.Topics[i].Origin == nil {
				c.Topics[i].Origin = &unknown
			}
		}
		if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='circles' AND id=?", dbutil.JSON(c), c.ID); err != nil {
			return err
		}
		var raw string
		err = tx.QueryRow("SELECT data FROM objects WHERE bucket='proposals' AND id=?", c.ID).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var p model.Proposal
			if err = json.Unmarshal([]byte(raw), &p); err != nil {
				return err
			}
			p.Circle = c
			if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='proposals' AND id=?", dbutil.JSON(p), c.ID); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec("INSERT INTO hub_schema VALUES(5)"); err != nil {
		return err
	}
	return tx.Commit()
}
