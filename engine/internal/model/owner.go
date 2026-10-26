package model

import "time"

type OwnerPerson struct {
	Name     string  `json:"name"`
	Relation string  `json:"relation"`
	PersonID *string `json:"person_id"`
}
type OwnerProject struct {
	Name string   `json:"name"`
	With []string `json:"with"`
}
type OwnerSuggestion struct {
	ID     string    `json:"id"`
	Field  string    `json:"field"`
	Value  any       `json:"value"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}
type OwnerProfile struct {
	Name        string            `json:"name"`
	About       string            `json:"about"`
	Studies     string            `json:"studies"`
	Work        string            `json:"work"`
	People      []OwnerPerson     `json:"people"`
	Projects    []OwnerProject    `json:"projects"`
	UpdatedBy   string            `json:"updated_by"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Suggestions []OwnerSuggestion `json:"suggestions"`
	Person      *Person           `json:"person,omitempty"`
}

func (p OwnerProfile) Thin() bool { return p.Studies == "" && p.Work == "" || len(p.People) == 0 }
