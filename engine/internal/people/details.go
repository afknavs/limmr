package people

import (
	"bytes"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"time"
)

// Derive only after chat projection, so a group-only reader never learns a private JID.
func withDerivedPhones(p model.Person) model.Person {
	p.Details.Phones = append([]model.LabeledValue{}, p.Details.Phones...)
	if p.Details.Emails == nil {
		p.Details.Emails = []model.LabeledValue{}
	}
	if p.Details.Handles == nil {
		p.Details.Handles = []model.LabeledValue{}
	}
	if p.DetailSuggestions == nil {
		p.DetailSuggestions = []model.DetailSuggestion{}
	}
	for _, jid := range p.JIDs {
		if !strings.HasSuffix(jid, "@s.whatsapp.net") {
			continue
		}
		value := "+" + strings.SplitN(jid, "@", 2)[0]
		found := false
		for _, v := range p.Details.Phones {
			if v.Value == value {
				found = true
			}
		}
		if !found {
			p.Details.Phones = append(p.Details.Phones, model.LabeledValue{Label: "mobile", Value: value, Source: "whatsapp"})
		}
	}
	return p
}

// The patch contains only supplied fields. Agent values are queued separately and
// cannot replace even a single owner-entered field, regardless of their claimed source.
func applyDetails(p *model.Person, raw any, a *model.Agent) error {
	b, e := json.Marshal(raw)
	if e != nil || len(b) > 16000 {
		return model.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(b, &fields); e != nil || fields == nil {
		return model.ErrInvalid
	}
	for field, value := range fields {
		switch field {
		case "sources":
			// Provenance is derived from unchanged values, never trusted from a client.
			continue
		case "phones", "emails", "handles":
			var xs []model.LabeledValue
			d := json.NewDecoder(bytes.NewReader(value))
			d.DisallowUnknownFields()
			if e = d.Decode(&xs); e != nil || len(xs) > 50 {
				return model.ErrInvalid
			}
			for i := range xs {
				if strings.TrimSpace(xs[i].Value) == "" || len(xs[i].Value) > 1000 || len(xs[i].Label) > 100 {
					return model.ErrInvalid
				}
				xs[i].Source = "you"
				if a == nil {
					var old []model.LabeledValue
					switch field {
					case "phones":
						old = p.Details.Phones
					case "emails":
						old = p.Details.Emails
					case "handles":
						old = p.Details.Handles
					}
					for _, v := range old {
						if v.Label == xs[i].Label && v.Value == xs[i].Value {
							xs[i].Source = v.Source
						}
					}
				}
				if a != nil {
					proposeDetail(p, field, xs[i].Label, xs[i].Value, a.ID)
				}
			}
			if a == nil {
				if p.Details.Sources == nil {
					p.Details.Sources = map[string]string{}
				}
				p.Details.Sources[field] = "you"
				switch field {
				case "phones":
					p.Details.Phones = xs
				case "emails":
					p.Details.Emails = xs
				case "handles":
					p.Details.Handles = xs
				}
			}
		case "birthday", "location", "pronouns", "org", "title":
			var v string
			if e = json.Unmarshal(value, &v); e != nil || len(v) > 1000 {
				return model.ErrInvalid
			}
			if field == "birthday" && v != "" {
				date := v
				if len(v) == 5 {
					date = "2000-" + v
				}
				if _, e = time.Parse("2006-01-02", date); e != nil {
					return model.ErrInvalid
				}
			}
			if a != nil {
				if v != "" {
					proposeDetail(p, field, "", v, a.ID)
				}
			} else {
				if p.Details.Sources == nil {
					p.Details.Sources = map[string]string{}
				}
				previous := ""
				switch field {
				case "birthday":
					previous = p.Details.Birthday
				case "org":
					previous = p.Details.Org
				case "title":
					previous = p.Details.Title
				case "location":
					previous = p.Details.Location
				case "pronouns":
					previous = p.Details.Pronouns
				}
				if previous != v || p.Details.Sources[field] == "" {
					p.Details.Sources[field] = "you"
				}
				switch field {
				case "org":
					p.Details.Org = v
				case "title":
					p.Details.Title = v
				case "birthday":
					p.Details.Birthday = v
				case "location":
					p.Details.Location = v
				case "pronouns":
					p.Details.Pronouns = v
				}
			}
		default:
			return model.ErrInvalid
		}
	}
	if len(p.DetailSuggestions) > 200 {
		return model.ErrInvalid
	}
	return nil
}
func proposeDetail(p *model.Person, field, label, value, source string) {
	for _, s := range p.DetailSuggestions {
		if s.Field == field && s.Label == label && s.Value == value && s.Source == source {
			return
		}
	}
	p.DetailSuggestions = append(p.DetailSuggestions, model.DetailSuggestion{ID: dbutil.ID(), Field: field, Label: label, Value: value, Source: source, Status: "suggested"})
}
func (s *Service) ReviewDetail(id, suggestion string, confirm bool) (model.Person, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var p model.Person
	if e := s.Hub.Repo.Get("people", id, &p); e != nil {
		return p, e
	}
	for i, v := range p.DetailSuggestions {
		if v.ID != suggestion {
			continue
		}
		if confirm {
			raw := map[string]any{v.Field: v.Value}
			switch v.Field {
			case "phones", "emails", "handles":
				var xs []model.LabeledValue
				switch v.Field {
				case "phones":
					xs = p.Details.Phones
				case "emails":
					xs = p.Details.Emails
				case "handles":
					xs = p.Details.Handles
				}
				xs = append(xs, model.LabeledValue{Label: v.Label, Value: v.Value, Source: "you"})
				raw[v.Field] = xs
			}
			if v.Field == "how_known" {
				p.HowKnown = v.Value
			} else if v.Field == "notes" {
				p.Notes = v.Value
			} else if v.Field == "role" {
				p.Role = v.Value
			} else if e := applyDetails(&p, raw, nil); e != nil {
				return p, e
			}
		}
		p.DetailSuggestions = append(p.DetailSuggestions[:i], p.DetailSuggestions[i+1:]...)
		p.UpdatedAt = s.Hub.Now().UTC()
		if e := s.Hub.Repo.Put("people", id, p); e != nil {
			return p, e
		}
		e := s.Hub.Record(model.Activity{AgentID: "you", Tool: "review_person_detail", Kind: "people_write", Count: 1})
		return withDerivedPhones(p), e
	}
	return p, model.ErrNotFound
}
func (s *Service) ConfirmFact(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var f model.Fact
	if e := s.Hub.Repo.Get("facts", id, &f); e != nil {
		return e
	}
	if f.Status != "suggested" {
		return model.ErrConflict
	}
	f.Status = "active"
	f.Source = "you"
	return s.Hub.Repo.Put("facts", id, f)
}

// Forget removes owned profile data and reversible copies atomically. Message
// history is deliberately untouched. No WhatsApp client or send path is involved.
func (s *Service) Forget(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var p model.Person
	if e := s.Hub.Repo.Get("people", id, &p); e != nil {
		return e
	}
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return e
	}
	proposals, e := hub.List[model.Proposal](s.Hub.Repo, "proposals")
	if e != nil {
		return e
	}
	tx, e := s.Hub.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Collect live and previously deleted knowledge before scrubbing its history.
	// Relations remain hidden removed rows with an empty payload.
	rows, e := tx.Query(`SELECT DISTINCT bucket,id FROM
 (SELECT bucket,id,data FROM objects UNION ALL SELECT bucket,id,data FROM sync_deleted) WHERE
 (bucket='people' AND id=?) OR
 (bucket='facts' AND json_extract(data,'$.subject')='person' AND json_extract(data,'$.subject_id')=?) OR
 (bucket='relations' AND (json_extract(data,'$.a')=? OR json_extract(data,'$.b')=?)) OR
 (bucket='memberships' AND json_type(data,'$.group') IS NULL AND json_extract(data,'$.person')=?)`, id, id, id, id, id)
	if e != nil {
		return e
	}
	type forgotten struct{ bucket, id string }
	forgottenRows := []forgotten{}
	for rows.Next() {
		var row forgotten
		if e = rows.Scan(&row.bucket, &row.id); e != nil {
			break
		}
		forgottenRows = append(forgottenRows, row)
	}
	if err := rows.Err(); e == nil {
		e = err
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, q := range []string{
		`DELETE FROM objects WHERE bucket='changes' AND (json_extract(data,'$.subject_id')=? OR id IN (SELECT id FROM objects WHERE bucket='relations' AND (json_extract(data,'$.a')=? OR json_extract(data,'$.b')=?)))`,
		`DELETE FROM objects WHERE bucket='facts' AND json_extract(data,'$.subject')='person' AND json_extract(data,'$.subject_id')=?`,
		`DELETE FROM objects WHERE bucket='relations' AND (json_extract(data,'$.a')=? OR json_extract(data,'$.b')=?)`,
		`DELETE FROM objects WHERE bucket='memberships' AND json_extract(data,'$.person')=?`,
		`DELETE FROM objects WHERE bucket='people' AND id=?`,
		`DELETE FROM objects WHERE bucket='stats' AND id=?`,
		`DELETE FROM objects WHERE bucket='person_summaries' AND id=?`,
		`DELETE FROM objects WHERE bucket='relationship_inferences' AND id=?`,
	} {
		args := []any{}
		for n := 0; n < strings.Count(q, "?"); n++ {
			args = append(args, id)
		}
		if _, e = tx.Exec(q, args...); e != nil {
			return e
		}
	}
	remove := func(c model.Circle) model.Circle {
		people := []model.CirclePerson{}
		for _, v := range c.People {
			if v.PersonID != id {
				people = append(people, v)
			}
		}
		c.People = people
		for i := range c.Topics {
			ids := []string{}
			for _, v := range c.Topics[i].People {
				if v != id {
					ids = append(ids, v)
				}
			}
			c.Topics[i].People = ids
		}
		return c
	}
	for _, c := range cs {
		c = remove(c)
		if _, e = tx.Exec("UPDATE objects SET data=? WHERE bucket='circles' AND id=?", dbutil.JSON(c), c.ID); e != nil {
			return e
		}
	}
	for _, p := range proposals {
		p.Circle = remove(p.Circle)
		if _, e = tx.Exec("UPDATE objects SET data=? WHERE bucket='proposals' AND id=?", dbutil.JSON(p), p.ID); e != nil {
			return e
		}
	}
	for _, row := range forgottenRows {
		if e = hub.ForgetSyncedObject(tx, row.bucket, row.id); e != nil {
			return e
		}
	}
	at := s.Hub.Now().UTC()
	activity := model.Activity{At: at, AgentID: "you", Tool: "forget_person", Kind: "people_write", Count: 1, ChatJIDs: []string{}, MessageIDs: []string{}, Note: "Forgot a person and their profile, facts, relations and circle memberships."}
	if _, e = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", at, dbutil.JSON(activity)); e != nil {
		return e
	}
	return tx.Commit()
}
