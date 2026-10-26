package people

import (
	"limmr/internal/hub"
	"limmr/internal/model"
)

// OwnerCircleIDs is the metadata-only owner projection. Listing circles needs
// identity visibility, not DM content tokenisation or affinity enrichment.
// Agent callers continue through the full permission-scoped PeopleAll path.
func (s *Service) OwnerCircleIDs() (map[string]bool, error) {
	key := s.cacheKey(nil, "owner-circle-ids")
	shared := s.shared()
	shared.cacheMu.Lock()
	if shared.ownerIDs != nil && shared.ownerIDsKey == key {
		out := copyIDs(shared.ownerIDs)
		shared.cacheMu.Unlock()
		return out, nil
	}
	shared.cacheMu.Unlock()
	ps, err := s.directory()
	if err != nil {
		return nil, err
	}
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return nil, err
	}
	excluded := s.excludedSenders(nil, ps, cs)
	ids := map[string]bool{}
	for _, p := range ps {
		if !excluded[p.ID] && (model.Named(p) || model.Named(model.Person{Name: p.PushName})) {
			ids[p.ID] = true
		}
	}
	shared.cacheMu.Lock()
	shared.ownerIDsKey, shared.ownerIDs = key, copyIDs(ids)
	shared.cacheMu.Unlock()
	return ids, nil
}

func copyIDs(ids map[string]bool) map[string]bool {
	out := make(map[string]bool, len(ids))
	for id, v := range ids {
		out[id] = v
	}
	return out
}
