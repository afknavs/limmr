package hub

import "limmr/internal/model"

// An agent-specific restriction can narrow, never bypass, the circle policy.
func (s *Service) AgentSendPolicy(a model.Agent, chat string) (string, error) {
	rule, e := s.SendPolicy(chat)
	if e != nil {
		return "", e
	}
	var override string
	if err := s.Repo.Get("group_send_rules", a.ID+"/"+chat, &override); err != nil && err != model.ErrNotFound {
		return "", err
	}
	if override == "never" || rule == "never" {
		return "never", nil
	}
	if override == "approval" && rule == "free" {
		return "confirm", nil
	}
	return rule, nil
}
