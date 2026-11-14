package tasks

import "encoding/json"

// NeedsYouPeople projects the same expiry/end-state rules as task cards without
// loading workspaces, running a tick, or changing any stored record.
func (s *Service) NeedsYouPeople() (map[string]bool, error) {
	out := map[string]bool{}
	rows, err := s.Hub.Repo.DB.Query(`SELECT data FROM task_cards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []record{}
	for rows.Next() {
		var raw string
		var r record
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// present can read effort membership/settings. Release the hub's only
	// connection before projecting any card, including legacy memberships.
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for _, r := range records {
		if s.authorize(nil, r.Task, false) != nil {
			continue
		}
		t := s.present(nil, r)
		if t.NeedsYou {
			for _, p := range t.People {
				out[p.ID] = true
			}
		}
	}
	return out, nil
}
