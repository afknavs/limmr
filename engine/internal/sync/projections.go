package sync

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"sort"
)

// Slot state and Suggestions are projections of immutable replicated events.
// The earliest claim wins; owner edits/decisions for that claim use the latest
// event. Rebuilding a projection never creates a fresh replication edit.
type scheduledClaim struct {
	ID         string
	TaskID     string
	HLC        int64
	Origin     string
	Slot       string         `json:"slot"`
	Suggestion map[string]any `json:"suggestion"`
}

func reconcileSlots(tx *sql.Tx, touched map[string]bool) error {
	rows, err := tx.Query(`SELECT e.id,e.task_id,e.kind,e.payload,m.hlc,m.origin FROM task_events e JOIN sync_meta m ON m.bucket='task_events' AND m.id=e.id WHERE e.kind IN ('slot_claim','slot_sent','slot_suggestion') AND m.op='upsert' ORDER BY m.hlc,m.origin,e.id`)
	if err != nil {
		return err
	}
	claims := []scheduledClaim{}
	latest := map[string]map[string]any{}
	sent := map[string]bool{}
	for rows.Next() {
		var c scheduledClaim
		var kind, raw string
		if err = rows.Scan(&c.ID, &c.TaskID, &kind, &raw, &c.HLC, &c.Origin); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			break
		}
		id, _ := c.Suggestion["id"].(string)
		if c.Slot == "" {
			continue
		}
		switch kind {
		case "slot_claim":
			if id == "" {
				continue
			}
			claims = append(claims, c)
			latest[id] = c.Suggestion
		case "slot_suggestion":
			if id != "" {
				latest[id] = c.Suggestion
			}
		case "slot_sent":
			sent[c.Slot] = true
		}
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	winners := map[string]string{}
	for _, c := range claims {
		id, _ := c.Suggestion["id"].(string)
		state, reason := "kept", ""
		if winners[c.Slot] == "" {
			winners[c.Slot] = c.ID
		} else {
			state, reason = "dismissed", "duplicate_after_sync"
		}
		if _, err = tx.Exec(`INSERT INTO sync_slot_claims VALUES(?,?,?,?,?,?) ON CONFLICT(claim_id) DO UPDATE SET state=excluded.state,reason=excluded.reason`, c.ID, c.Slot, c.HLC, c.Origin, state, reason); err != nil {
			return err
		}
		v := latest[id]
		v["task_id"] = c.TaskID
		// Draft credentials refer to this engine's agents; a replicated draft remains
		// visible for its owner but does not carry a peer's machine-local identity.
		if state == "dismissed" {
			v["state"] = "dismissed"
			v["reason"] = reason
		} else if sent[c.Slot] {
			v["state"] = "sent"
			delete(v, "reason")
		}
		if _, err = tx.Exec(`INSERT INTO objects VALUES('suggestions',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data`, id, dbutil.JSON(v)); err != nil {
			return err
		}
		// Any parked delivery draft belonging to the losing suggestion must also stop
		// appearing as a live owner action. No send or WhatsApp method is invoked.
		if state == "dismissed" {
			if _, err = tx.Exec(`UPDATE objects SET data=json_set(data,'$.state','cancelled','$.reason','duplicate_after_sync') WHERE bucket='drafts' AND json_extract(data,'$.suggestion_id')=? AND json_extract(data,'$.state') IN ('draft','suggested')`, id); err != nil {
				return err
			}
		}
		touched[c.TaskID] = true
	}
	return nil
}

// Preserve the local effort cache; the task tick recalculates it from this
// engine's derived efforts. A peer's cached effort_needs is never accepted.
func localTaskDerived(tx *sql.Tx, id string, data json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	var raw string
	err := tx.QueryRow(`SELECT data FROM tasks WHERE id=?`, id).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var local map[string]any
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &local); err != nil {
			return nil, err
		}
	}
	delete(obj, "effort_needs")
	if local["effort_needs"] == true {
		obj["effort_needs"] = true
	}
	return json.Marshal(obj)
}

// Recompute every task touched by this batch, including stale self-echoes and
// event-only changes. A checklist removal therefore cannot leave one replica
// with the old derived total. The caller owns the encompassing transaction.
func recomputeTasks(tx *sql.Tx, touched map[string]bool) error {
	ids := make([]string, 0, len(touched))
	for id := range touched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var raw string
		err := tx.QueryRow(`SELECT data FROM tasks WHERE id=?`, id).Scan(&raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		var obj map[string]any
		if err = json.Unmarshal([]byte(raw), &obj); err != nil {
			return err
		}
		steps, _ := obj["checklist"].([]any)
		done := 0
		for _, step := range steps {
			if st, ok := step.(map[string]any); ok && st["done"] == true {
				done++
			}
		}
		obj["progress"] = map[string]int{"done": done, "total": len(steps)}
		var pending int
		if err = tx.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'`, id).Scan(&pending); err != nil {
			return err
		}
		flag, _ := obj["flag_reason"].(string)
		open := obj["status"] == "active" || obj["status"] == "waiting"
		needs := open && (pending > 0 || flag != "" || obj["effort_needs"] == true)
		obj["needs_you"] = needs
		delete(obj, "needs_you_reason")
		if needs {
			switch {
			case flag != "":
				obj["needs_you_reason"] = flag
			case pending > 0:
				obj["needs_you_reason"] = "An update is ready for your review"
			default:
				obj["needs_you_reason"] = "Your effort needs attention"
			}
		}
		if _, err = tx.Exec(`UPDATE tasks SET data=? WHERE id=?`, dbutil.JSON(obj), id); err != nil {
			return err
		}
	}
	return nil
}
