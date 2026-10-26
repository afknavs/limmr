package hub

import (
	"context"
	"encoding/json"
	"limmr/internal/model"
	"sort"
	"strings"
	"unicode/utf8"
)

func ValidateVariations(xs []string) error {
	if len(xs) > 3 {
		return model.ErrInvalid
	}
	for _, x := range xs {
		if strings.TrimSpace(x) == "" || utf8.RuneCountInString(x) > 1000 {
			return model.ErrInvalid
		}
	}
	return nil
}

// PresentSuggestion computes display-only values against current circle/agent settings.
func (s *Service) PresentSuggestion(x model.Suggestion) (model.Suggestion, error) {
	x.Body = x.DraftText
	xs := []string{x.Body}
	for i, v := range x.Variations {
		if i > 0 {
			xs = append(xs, v)
		}
	}
	x.Variations = xs
	return x, nil
}
func sortSuggestions(xs []model.Suggestion) {
	sort.Slice(xs, func(i, j int) bool {
		if !xs[i].CreatedAt.Equal(xs[j].CreatedAt) {
			return xs[i].CreatedAt.After(xs[j].CreatedAt)
		}
		return xs[i].ID < xs[j].ID
	})
}

// Owner review keeps the originating agent's
// enabled/send permission, current chat visibility, and strictest circle rule.
func (s *Service) ownerSendAgent(id, chat string) (model.Agent, string, error) {
	if id == "you" {
		a := model.Agent{ID: "you", Name: "You", Enabled: true, Permissions: model.Defaults(false)}
		if s.HiddenChat(chat) {
			return a, "", model.ErrNotFound
		}
		rule, err := s.SendPolicy(chat)
		if rule == "never" {
			err = model.ErrForbidden
		}
		return a, rule, err
	}

	a, e := s.Agent(id)
	if e != nil || !a.Enabled || !a.Permissions.RequestSend {
		return a, "", model.ErrForbidden
	}
	cs, _, e := s.Visible(&a)
	if e != nil {
		return a, "", e
	}
	found := false
	for _, c := range cs {
		if c.JID == chat {
			found = true
		}
	}
	if !found || !s.CanChat(&a, chat) {
		return a, "", model.ErrNotFound
	}
	rule, e := s.AgentSendPolicy(a, chat)
	if e == nil && rule == "never" {
		e = model.ErrForbidden
	}
	return a, rule, e
}

// Deprecated compatibility inputs are accepted without affecting delivery.
type SuggestionSend struct {
	Voice     json.RawMessage `json:"voice,omitempty"`
	Signed    json.RawMessage `json:"signed,omitempty"`
	Text      *string         `json:"text"`
	Variation *int            `json:"variation"`
}

// This capability is created only by the control-API owner review path, never
// from MCP input, headers, or persisted draft fields.
type ownerReviewKey struct{}

func isOwnerReview(ctx context.Context) bool {
	v, _ := ctx.Value(ownerReviewKey{}).(bool)
	return v
}
