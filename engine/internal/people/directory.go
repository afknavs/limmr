package people

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
)

// WhatsApp refreshes base identity fields without overwriting human/agent enrichment.
func (s *Service) ReplaceGroups(ctx context.Context, groups []model.Group, ms []model.Membership) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	tx, e := s.Hub.Repo.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM objects WHERE bucket IN ('groups','memberships')"); e != nil {
		return e
	}
	for _, g := range groups {
		if _, e = tx.Exec("INSERT INTO objects VALUES('groups',?,?)", g.JID, dbutil.JSON(g)); e != nil {
			return e
		}
	}
	for _, m := range ms {
		if _, e = tx.Exec("INSERT INTO objects VALUES('memberships',?,?)", m.Group+":"+m.Person, dbutil.JSON(m)); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// MarkBusiness changes metadata only: verified-business events cannot replace a
// phone contact's saved name. The no-op predicate avoids repeated invalidations.
func (s *Service) MarkBusiness(jid string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	_, e := s.Hub.Repo.DB.Exec(`UPDATE objects SET data=json_set(data,'$.business',json('true')) WHERE bucket='people' AND id IN (SELECT person_id FROM people_jids WHERE jid=?) AND COALESCE(json_extract(data,'$.business'),0)=0`, jid)
	return e
}

// directory caches decoded canonical identities, never an agent projection.
// SQLite's revision includes direct SQL, imports, deletes and atomic batches.
// Callers receive independent nested storage before applying scope/enrichment.
func (s *Service) directory() ([]model.Person, error) {
	var revision int64
	if err := s.Hub.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&revision); err != nil {
		return nil, err
	}
	shared := s.shared()
	shared.cacheMu.Lock()
	if shared.directoryLoaded && shared.directoryRevision == revision {
		out := clonePeople(shared.directoryPeople)
		shared.cacheMu.Unlock()
		return out, nil
	}
	shared.cacheMu.Unlock()
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return nil, err
	}
	shared.cacheMu.Lock()
	shared.directoryPeople, shared.directoryRevision, shared.directoryLoaded = clonePeople(ps), revision, true
	shared.cacheMu.Unlock()
	return ps, nil
}
