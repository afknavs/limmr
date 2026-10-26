package hub

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// Clear obsolete configuration atomically. Keep access, send rules, identities,
// receipts and dedupe keys. Delivered text and the activity log remain historical
// evidence; only pending generated previews lose their known appended suffix.
func (r *Repo) migratePlainSends() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=7").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}
	rows, err := tx.Query("SELECT bucket,id,data FROM objects WHERE bucket IN ('agents','circles','proposals','drafts','approvals','suggestions')")
	if err != nil {
		return err
	}
	type record struct {
		bucket, id string
		data       map[string]any
	}
	var records []record
	for rows.Next() {
		var x record
		var raw string
		if err = rows.Scan(&x.bucket, &x.id, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &x.data); err != nil {
			rows.Close()
			return err
		}
		records = append(records, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var owner struct {
		Owner string `json:"owner"`
	}
	var raw string
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='settings' AND id='main'").Scan(&raw); err == nil {
		if err = json.Unmarshal([]byte(raw), &owner); err != nil {
			return err
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	signatures := map[string]string{}
	modes := map[string]string{}
	for _, x := range records {
		if x.bucket != "agents" {
			continue
		}
		p, _ := x.data["permissions"].(map[string]any)
		template, _ := p["signature"].(string)
		modes[x.id], _ = p["signature_mode"].(string)
		name, _ := x.data["name"].(string)
		signatures[x.id] = strings.NewReplacer("{agent}", name, "{owner}", owner.Owner).Replace(template)
	}
	for _, x := range records {
		d := x.data
		switch x.bucket {
		case "agents":
			p, _ := d["permissions"].(map[string]any)
			// Missing, empty or invalid grants in a legacy permission object
			// never enabled sending. Modern objects have none of these keys.
			_, hasVoices := p["voices"]
			_, hasTemplate := p["signature"]
			_, hasMode := p["signature_mode"]
			if hasVoices || hasTemplate || hasMode {
				allowed := false
				if voices, ok := p["voices"].([]any); ok {
					for _, v := range voices {
						if v == "as_agent" || v == "as_me" {
							allowed = true
						}
					}
				}
				if !allowed {
					p["request_send"] = false
				}
			}
			clearSendFields(p)
		case "drafts", "approvals", "suggestions":
			state, _ := d["state"].(string)
			pending := state == "draft" || state == "suggested" || state == "pending"
			agent, _ := d["agent_id"].(string)
			if x.bucket == "suggestions" {
				agent, _ = d["created_by"].(string)
			}
			voice, _ := d["voice"].(string)
			signed, explicit := d["signed"].(bool)
			generated := signed || !explicit && voice == "as_agent" && modes[agent] != "never"
			if x.bucket == "suggestions" && (voice == "" || voice == "as_agent") {
				generated = true
			}
			if pending && generated {
				for _, field := range []string{"text", "final_text", "draft_text", "body"} {
					if text, ok := d[field].(string); ok {
						d[field] = stripLegacySuffix(text, signatures[agent])
					}
				}
				if xs, ok := d["variations"].([]any); ok {
					for i, v := range xs {
						if text, ok := v.(string); ok {
							xs[i] = stripLegacySuffix(text, signatures[agent])
						}
					}
				}
			}
		case "proposals":
			if c, ok := d["circle"].(map[string]any); ok {
				clearSendFields(c)
			}
		}
		clearSendFields(d)
		b, e := json.Marshal(d)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket=? AND id=?", string(b), x.bucket, x.id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE trigger_watches SET definition_json=json_remove(definition_json,'$.handoff.send_grant.voice'); INSERT INTO hub_schema VALUES(7);`); err != nil {
		return err
	}
	return tx.Commit()
}
func clearSendFields(d map[string]any) {
	for _, k := range []string{"signature", "signature_mode", "signature_preview", "voices", "voice", "voice_used", "voice_reason", "signed", "default_voice", "allowed_voices", "allow_as_me"} {
		delete(d, k)
	}
}
func stripLegacySuffix(text, suffix string) string {
	if suffix == "" {
		return text
	}
	for {
		trimmed := strings.TrimRight(text, " \t\r\n")
		if trimmed == suffix {
			return ""
		}
		if !strings.HasSuffix(trimmed, "\n"+suffix) {
			return text
		}
		text = strings.TrimSuffix(trimmed, "\n"+suffix)
	}
}
