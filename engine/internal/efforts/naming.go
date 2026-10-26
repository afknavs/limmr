package efforts

import (
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"sort"
	"strings"
)

// NamingCandidate deliberately excludes chat labels, full names and message text
// for agents. Only the control API includes the owner's six bounded snippets.
type NamingCandidate struct {
	ID                    string   `json:"id"`
	Revision              string   `json:"revision"`
	TopTerms              []string `json:"top_terms"`
	ArtefactNames         []string `json:"artefact_names"`
	MessageCount          int      `json:"message_count"`
	ChatCount             int      `json:"chat_count"`
	ParticipantCount      int      `json:"participant_count"`
	ParticipantFirstNames []string `json:"participant_first_names,omitempty"`
	Snippets              []string `json:"snippets,omitempty"`
}

func namingRevision(e Effort, c cached) string {
	ms := []model.Message{}
	for _, r := range e.MessageRefs {
		ms = append(ms, c.messages[key(r)])
	}
	return dbutil.Digest(dbutil.JSON([]any{e.ID, e.Title, e.Summary, e.Status, e.Hidden, e.People, ms}))
}

// Unnamed requires visibility of every chat in the owner-visible effort. Inferring
// a smaller agent-only cluster here could leak partial names or invite bad edits.
func (s *Service) Unnamed(a *model.Agent, limit int) ([]NamingCandidate, int, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	out := []NamingCandidate{}
	if a != nil && (!a.Permissions.ReadMessages || !a.Permissions.ReadPeople) {
		return out, 0, model.ErrForbidden
	}
	visible, err := s.visible(a)
	if err != nil {
		return out, 0, err
	}
	c, err := s.snapshot(nil)
	if err != nil {
		return out, 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	total := 0
	for _, e := range c.efforts {
		if e.Hidden || goodTitle(e.Title) {
			continue
		}
		safe := true
		for _, chat := range e.Chats {
			if _, ok := visible[chat.ID]; !ok {
				safe = false
			}
		}
		if !safe {
			continue
		}
		total++
		if len(out) >= limit {
			continue
		}
		n := NamingCandidate{ID: e.ID, Revision: namingRevision(e, c), TopTerms: []string{}, ArtefactNames: []string{}, MessageCount: len(e.MessageRefs), ChatCount: len(e.Chats), ParticipantCount: len(e.People)}
		counts := map[string]int{}
		files := map[string]bool{}
		for _, r := range e.MessageRefs {
			m := c.messages[key(r)]
			for term := range c.groups.rules.terms(m) {
				counts[term]++
			}
			if goodFile(m.Filename) {
				files[m.Filename] = true
			}
		}
		for term := range counts {
			n.TopTerms = append(n.TopTerms, term)
		}
		sort.Slice(n.TopTerms, func(i, j int) bool {
			a, b := n.TopTerms[i], n.TopTerms[j]
			if counts[a] == counts[b] {
				return a < b
			}
			return counts[a] > counts[b]
		})
		n.TopTerms = n.TopTerms[:min(12, len(n.TopTerms))]
		for file := range files {
			n.ArtefactNames = append(n.ArtefactNames, file)
		}
		sort.Strings(n.ArtefactNames)
		n.ArtefactNames = n.ArtefactNames[:min(8, len(n.ArtefactNames))]
		if a == nil {
			for _, p := range e.People {
				if ws := strings.Fields(p.Name); len(ws) > 0 {
					n.ParticipantFirstNames = append(n.ParticipantFirstNames, ws[0])
				}
			}
			for i := len(e.MessageRefs) - 1; i >= 0 && len(n.Snippets) < 6; i-- {
				m := c.messages[key(e.MessageRefs[i])]
				if text := strings.TrimSpace(m.Content); text != "" {
					speaker := "Participant"
					if m.IsFromMe {
						speaker = "You"
					} else if ws := strings.Fields(c.names[m.Sender].Name); len(ws) > 0 {
						speaker = ws[0]
					}
					n.Snippets = append(n.Snippets, clip(speaker+": "+text, 160))
				}
			}
		}
		out = append(out, n)
	}
	return out, total, nil
}

func NamingHint(total int) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d efforts waiting for names. Use more efforts.unnamed, then efforts.update with a title and summary.", total)
}
