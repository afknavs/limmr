// Package triggers batches incoming messages into watches and explicitly granted
// session handoffs. It has no WhatsApp client or send implementation.
package triggers

import (
	"context"
	"errors"
	"time"
)

type Status string

const (
	Active  Status = "active"
	Paused  Status = "paused"
	Stopped Status = "stopped"
	Expired Status = "expired"
)

type Action string

const (
	Feed           Action = "feed"
	Notify         Action = "notify"
	Digest         Action = "digest"
	ParkSuggestion Action = "park_suggestion"
	WakeSession    Action = "wake_session"
)

type MatchKind string

const (
	Any        MatchKind = "any"
	Keywords   MatchKind = "keywords"
	Regex      MatchKind = "regex"
	MentionsMe MatchKind = "mentions_me"
	FromPeople MatchKind = "from_people"
)

type Scope struct {
	All     bool     `json:"all,omitempty"`
	Chats   []string `json:"chats,omitempty"`
	Circles []string `json:"circles,omitempty"`
	People  []string `json:"people,omitempty"`
}
type Match struct {
	Kind     MatchKind `json:"kind"`
	Keywords []string  `json:"keywords,omitempty"`
	Regex    string    `json:"regex,omitempty"`
	People   []string  `json:"people,omitempty"`
}
type Instruction struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}
type Provenance struct {
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	GrantText string    `json:"grant_text"`
	At        time.Time `json:"at"`
}
type Watch struct {
	ID           string        `json:"id"`
	Title        string        `json:"title,omitempty"`
	Owner        string        `json:"owner"`
	Goal         string        `json:"goal"`
	Scope        Scope         `json:"scope"`
	Match        Match         `json:"match"`
	Action       Action        `json:"action"`
	Instructions []Instruction `json:"instructions"`
	Status       Status        `json:"status"`
	ExpiresAt    *time.Time    `json:"expires_at,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	CreatedVia   Provenance    `json:"created_via"`
	QuietWindow  time.Duration `json:"quiet_window"`
	MaxWindow    time.Duration `json:"max_window"`
}
type RunnerConfig struct {
	Kind      string `json:"kind"` // codex or claude_code
	SessionID string `json:"session_id"`
	Workdir   string `json:"workdir"`
}
type SendGrant struct {
	ChatJID string `json:"chat_jid"`
}
type QuietHours struct {
	Start    string `json:"start"`    // HH:MM, inclusive
	End      string `json:"end"`      // HH:MM, exclusive; equal means all day
	Timezone string `json:"timezone"` // IANA name, or Local
}
type Limits struct {
	MaxRepliesPerHour int         `json:"max_replies_per_hour"`
	QuietHours        *QuietHours `json:"quiet_hours,omitempty"`
	MaxRuns           int         `json:"max_runs"`
}
type HandoffConfig struct {
	Runner    RunnerConfig `json:"runner"`
	SendGrant SendGrant    `json:"send_grant"`
	Limits    Limits       `json:"limits"`
	Escalate  []string     `json:"escalate"`
	StopWords []string     `json:"stop_words"`
}
type Handoff struct {
	Watch
	HandoffConfig
}

// Record is the persistence envelope. A handoff has exactly one Watch, plus its
// optional handoff configuration; instructions and status have no duplicate copy.
type Record struct {
	Watch
	Handoff *HandoffConfig `json:"handoff,omitempty"`
}
type IncomingMessage struct {
	ID             string    `json:"id,omitempty"`
	ChatJID        string    `json:"chat_jid"`
	Sender         string    `json:"sender"`
	SenderPersonID string    `json:"sender_person_id,omitempty"`
	CircleIDs      []string  `json:"circle_ids,omitempty"`
	Text           string    `json:"text"`
	At             time.Time `json:"at"`
	FromMe         bool      `json:"from_me"`
	FromLimmr      bool      `json:"from_limmr"`
	MentionsMe     bool      `json:"mentions_me"` // derived from WhatsApp mention metadata
}
type Hit struct {
	ID       string            `json:"id"`
	WatchID  string            `json:"watch_id"`
	At       time.Time         `json:"at"`
	Messages []IncomingMessage `json:"messages"`
	Action   Action            `json:"action"`
	State    string            `json:"state"` // recorded, completed, failed, cancelled
}
type RunRecord struct {
	ID         string    `json:"id"`
	HandoffID  string    `json:"handoff_id"`
	HitID      string    `json:"hit_id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	ExitCode   int       `json:"exit_code"`
	TimedOut   bool      `json:"timed_out"`
	LogPath    string    `json:"log_path,omitempty"`
	State      string    `json:"state"` // running, completed, failed, cancelled
}

var (
	ErrNotFound = errors.New("trigger not found")
	ErrExists   = errors.New("trigger already exists")
	ErrClosed   = errors.New("trigger engine is closed")
)

// Store.Update and ReserveReply must be atomic. Run entries are upserted by ID;
// runs include the running reservation, so max-runs survives runtime restarts.
// Hits and Runs return oldest first; limit <= 0 means all.
type Store interface {
	Create(context.Context, Record) error
	Get(context.Context, string) (Record, error)
	List(context.Context) ([]Record, error)
	Update(context.Context, string, func(*Record) error) error
	Delete(context.Context, string) error
	PutHit(context.Context, Hit) error
	Hits(context.Context, string, int) ([]Hit, error)
	PutRun(context.Context, RunRecord) error
	Runs(context.Context, string) ([]RunRecord, error)
	ReplyCount(context.Context, string, time.Time) (int, error)
	ReserveReply(context.Context, string, time.Time, int) (bool, error)
}
type Notification struct {
	WatchID string
	Kind    string // hit, paused, expired, run_failed
	Hit     *Hit
}
type Notifier interface {
	Notify(context.Context, Notification) error
}
type Suggester interface {
	Suggest(context.Context, Record, Hit) error
}
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type RunRequest struct {
	ID        string
	HandoffID string
	Runner    RunnerConfig
	Prompt    string
	// Minted by the engine for this run only, never inherited from the caller.
	MCPToken string
	MCPURL   string
}
type RunResult struct {
	ExitCode int
	TimedOut bool
	LogPath  string
}
type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}
