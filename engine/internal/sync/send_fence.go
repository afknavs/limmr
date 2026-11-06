package sync

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
)

// A higher authority epoch can release an unconsumed local claim atomically
// with fencing that executor. It cannot release an uncertain external attempt or
// a remote claim whose executor's durable outcome has not arrived yet.
func (s *Service) fenceLocalClaims(tx *sql.Tx, epoch uint64) error {
	rows, e := tx.Query(`SELECT data FROM objects WHERE bucket='send_queue' AND json_extract(data,'$.phase') IN ('claimed','uncertain')`)
	if e != nil {
		return e
	}
	var claims []QueuedSend
	for rows.Next() {
		var raw string
		var q QueuedSend
		if e = rows.Scan(&raw); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(raw), &q); e != nil {
			break
		}
		claims = append(claims, q)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, q := range claims {
		if q.Phase == "uncertain" || q.Holder != s.EngineID || q.Epoch >= epoch {
			return model.ErrConflict
		}
	}
	for _, q := range claims {
		q.Phase = "queued"
		q.Holder = ""
		q.Epoch = epoch
		if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='send_queue' AND id=?`, dbutil.JSON(q), q.ID); e != nil {
			return e
		}
	}
	return nil
}
