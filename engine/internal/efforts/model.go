// Package efforts derives purpose-level history locally. It never sends or marks messages read.
package efforts

import (
	"limmr/internal/model"
	"time"
)

type Ref struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
}
type Entity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Effort struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	Status          string     `json:"status"`
	Due             *time.Time `json:"due,omitempty"`
	People          []Entity   `json:"people"`
	Chats           []Entity   `json:"chats"`
	CircleIDs       []string   `json:"circle_ids"`
	MessageRefs     []Ref      `json:"message_refs"`
	AgentActionRefs []int64    `json:"agent_action_refs"`
	FirstAt         time.Time  `json:"first_at"`
	LastAt          time.Time  `json:"last_at"`
	CreatedBy       string     `json:"created_by"`
	Confidence      float64    `json:"confidence"`
	Pinned          bool       `json:"pinned"`
	Hidden          bool       `json:"hidden"`
	// Search text is transient, never serialized or persisted.
	search         string
	titleSource    string
	titleAutomatic bool
	// Keep the proposal separate so incremental runs can re-resolve collisions
	// identically to a cold recompute when other efforts arrive or disappear.
	baseTitle       string
	baseTitleSource string
}
type TimelineMessage struct {
	model.Message
	Content string `json:"content"`
}

type Item struct {
	ID      string           `json:"id"`
	At      time.Time        `json:"at"`
	Message *TimelineMessage `json:"message,omitempty"`
	Action  *model.Activity  `json:"action,omitempty"`
}
type Detail struct {
	Effort
	Timeline []Item `json:"timeline"`
}
type Filter struct {
	Status, Query, Circle, Person string
	Limit                         int
	IncludeUntitled               bool
}
type Update struct {
	OnlyUnnamed      bool       `json:"-"`
	ExpectedLastAt   *time.Time `json:"-"`
	ExpectedRevision string     `json:"-"`
	Title            *string    `json:"title,omitempty"`
	Summary          *string    `json:"summary,omitempty"`
	Status           *string    `json:"status,omitempty"`
	Due              *string    `json:"due,omitempty"`
	Pinned           *bool      `json:"pinned,omitempty"`
	Hidden           *bool      `json:"hidden,omitempty"`
}
type override struct {
	StatusThrough *time.Time `json:"status_through,omitempty"`
	TitleOrigin   string     `json:"title_origin,omitempty"`
	Revision      int64      `json:"revision"`
	ID            string     `json:"id"`
	Update        Update     `json:"update"`
	Refs          []Ref      `json:"refs"`
	Excluded      []Ref      `json:"excluded"`
	ReplacedBy    string     `json:"replaced_by,omitempty"`
	By            string     `json:"by"`
}
type CircleUpdate struct {
	Circle   Entity     `json:"circle"`
	EffortID string     `json:"effort_id"`
	Title    string     `json:"title"`
	Status   string     `json:"status"`
	Line     string     `json:"line"`
	Due      *time.Time `json:"due,omitempty"`
	LastAt   time.Time  `json:"last_at"`
	Changed  bool       `json:"changed"`
}
