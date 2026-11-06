// Package tasks stores owner workspaces. Timers only queue Suggestions; they never send.
package tasks

import (
	"limmr/internal/efforts"
	"time"
)

type Entity = efforts.Entity
type Ref = efforts.Ref
type Credit struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}
type Summary struct {
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}
type OwnerNote struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Item struct {
	AIName        string    `json:"ai_name,omitempty"`
	Description   string    `json:"description,omitempty"`
	OCRText       string    `json:"ocr_text,omitempty"`
	MediaType     string    `json:"media_type,omitempty"`
	Sender        string    `json:"sender,omitempty"`
	Chat          string    `json:"chat,omitempty"`
	Caption       string    `json:"caption,omitempty"`
	Width         int       `json:"width,omitempty"`
	Height        int       `json:"height,omitempty"`
	NamedOnDevice bool      `json:"named_on_device,omitempty"`
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	URL           string    `json:"url,omitempty"`
	AttachmentID  string    `json:"attachment_id,omitempty"`
	Text          string    `json:"text,omitempty"`
	Source        string    `json:"source"`
	MessageRef    *Ref      `json:"message_ref,omitempty"`
	AddedBy       string    `json:"added_by"`
	AddedAt       time.Time `json:"added_at"`
	Label         string    `json:"label,omitempty"`
}
type Step struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Done   bool       `json:"done"`
	DoneAt *time.Time `json:"done_at,omitempty"`
	Order  int        `json:"order"`
}
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
type CheckIn struct {
	Rule      string     `json:"rule"`
	Weekday   *int       `json:"weekday,omitempty"`
	EveryDays int        `json:"every_days,omitempty"`
	AtTime    string     `json:"at_time"`
	To        string     `json:"to"`
	Note      string     `json:"note"`
	NextAt    time.Time  `json:"next_at"`
	LastAt    *time.Time `json:"last_at,omitempty"`
}
type Waiting struct {
	On                *Entity   `json:"on"`
	What              string    `json:"what"`
	Since             time.Time `json:"since"`
	NudgeAfterDays    int       `json:"nudge_after_days"`
	NudgeState        string    `json:"nudge_state"`
	NudgeSuggestionID string    `json:"nudge_suggestion_id,omitempty"`
}
type SentUpdate struct {
	At          time.Time `json:"at"`
	ChatJID     string    `json:"chat_jid"`
	MessageID   string    `json:"message_id"`
	TextPreview string    `json:"text_preview"`
	Kind        string    `json:"kind"`
	By          string    `json:"by"`
}
type Suggestion struct {
	Signals    []string  `json:"signals"`
	Confidence float64   `json:"confidence"`
	DetectedAt time.Time `json:"detected_at"`
	EffortIDs  []string  `json:"effort_ids"`
}

type LatestMessage struct {
	ChatJID   string    `json:"chat_jid"`
	MessageID string    `json:"message_id"`
	Chat      string    `json:"chat"`
	Sender    string    `json:"sender"`
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
}
type SummaryAttempt struct {
	ChatJID   string    `json:"chat_jid"`
	MessageID string    `json:"message_id"`
	Timestamp time.Time `json:"timestamp"`
}
type CircleSuggestion struct {
	CircleID string `json:"circle_id"`
	Reason   string `json:"reason"`
}
type Task struct {
	CircleSuggestions []CircleSuggestion `json:"circle_suggestions,omitempty"`
	CirclesEdited     bool               `json:"circles_edited,omitempty"`
	OwnerNotes        []OwnerNote        `json:"owner_notes,omitempty"`
	Cover             *Item              `json:"cover,omitempty"`
	CoverItemID       string             `json:"cover_item_id,omitempty"`
	CoverRemoved      bool               `json:"cover_removed,omitempty"`
	EndAt             *time.Time         `json:"end_at,omitempty"`
	EndCondition      string             `json:"end_condition,omitempty"`
	LastActiveAt      time.Time          `json:"last_active_at"`
	Latest            []LatestMessage    `json:"latest,omitempty"`
	SummaryAttempt    *SummaryAttempt    `json:"summary_attempt,omitempty"`
	History           []Event            `json:"history,omitempty"`
	Origin            string             `json:"origin"`
	Suggestion        *Suggestion        `json:"suggestion,omitempty"`
	AutoKeptAt        *time.Time         `json:"auto_kept_at,omitempty"`
	UndoChangeID      string             `json:"undo_change_id,omitempty"`
	ID                string             `json:"id"`
	Title             string             `json:"title"`
	Goal              string             `json:"goal"`
	Kind              string             `json:"kind"`
	Status            string             `json:"status"`
	EffortID          string             `json:"effort_id,omitempty"`
	Chats             []Entity           `json:"chats"`
	People            []Entity           `json:"people"`
	CircleIDs         []string           `json:"circle_ids"`
	CreatedBy         Credit             `json:"created_by"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	ClosedAt          *time.Time         `json:"closed_at,omitempty"`
	CloseReason       string             `json:"close_reason,omitempty"`
	Summary           Summary            `json:"summary"`
	Items             []Item             `json:"items"`
	Checklist         []Step             `json:"checklist"`
	Progress          Progress           `json:"progress"`
	CheckIn           *CheckIn           `json:"check_in,omitempty"`
	Waiting           *Waiting           `json:"waiting,omitempty"`
	LifespanDays      int                `json:"lifespan_days,omitempty"`
	UpdatesSent       []SentUpdate       `json:"updates_sent"`
	NeedsYou          bool               `json:"needs_you"`
	NeedsYouReason    string             `json:"needs_you_reason,omitempty"`
}

// Record-only fields never cross API boundaries.
type record struct {
	eventActor         string
	CoverReviewPending bool `json:"cover_review_pending,omitempty"`
	Task
	PauseChecklistClose bool   `json:"pause_checklist_close,omitempty"`
	DraftCheckIn        string `json:"draft_check_in,omitempty"`
	DraftSlot           string `json:"draft_slot,omitempty"`
	DraftNudge          string `json:"draft_nudge,omitempty"`
	FlagReason          string `json:"flag_reason,omitempty"`
	EffortNeeds         bool   `json:"effort_needs,omitempty"`
	EffortAcknowledged  bool   `json:"effort_acknowledged,omitempty"`
	Purged              bool   `json:"purged,omitempty"`
}
type WaitingSummary struct {
	OnName string    `json:"on_name"`
	What   string    `json:"what"`
	Since  time.Time `json:"since"`
	Days   int       `json:"days"`
}
type TaskSummary struct {
	CoverCandidate    *CoverCandidate    `json:"cover_candidate,omitempty"`
	ClosedAt          *time.Time         `json:"closed_at,omitempty"`
	CloseReason       string             `json:"close_reason,omitempty"`
	CircleSuggestions []CircleSuggestion `json:"circle_suggestions,omitempty"`
	Cover             *Item              `json:"cover,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	LastActiveAt      time.Time          `json:"last_active_at"`
	CircleIDs         []string           `json:"circle_ids"`
	Origin            string             `json:"origin"`
	Suggestion        *Suggestion        `json:"suggestion,omitempty"`
	CreatedBy         Credit             `json:"created_by"`
	AutoKeptAt        *time.Time         `json:"auto_kept_at,omitempty"`
	UndoChangeID      string             `json:"undo_change_id,omitempty"`
	ID                string             `json:"id"`
	Title             string             `json:"title"`
	Kind              string             `json:"kind"`
	Status            string             `json:"status"`
	Progress          Progress           `json:"progress"`
	NextCheckInAt     *time.Time         `json:"next_check_in_at,omitempty"`
	Waiting           *WaitingSummary    `json:"waiting,omitempty"`
	NeedsYou          bool               `json:"needs_you"`
	NeedsYouReason    string             `json:"needs_you_reason,omitempty"`
	LastUpdateAt      *time.Time         `json:"last_update_at,omitempty"`
	EffortID          string             `json:"effort_id,omitempty"`
}
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}
type FeedItem struct {
	TaskSummary
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	Events    []Event    `json:"events"`
}
