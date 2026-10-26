// Package model defines the stable snake_case control and MCP contract.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// PersonID keeps phone/LID identifiers out of group-only profile IDs.
func PersonID(jid string) string {
	sum := sha256.Sum256([]byte(jid))
	return "person-" + hex.EncodeToString(sum[:12])
}

var (
	ErrUpdating  = errors.New("Limmr is updating; try again in a minute")
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("permission denied")
	ErrInvalid   = errors.New("invalid request")
	ErrConflict  = errors.New("operation conflicts with current state")
)

type Scope struct {
	Mode string   `json:"mode"`
	JIDs []string `json:"jids"`
}
type Permissions struct {
	Sync          bool   `json:"sync"`
	Access        Access `json:"access"`
	Chats         Scope  `json:"chats"`
	ReadMessages  bool   `json:"read_messages"`
	Search        bool   `json:"search"`
	ReadPeople    bool   `json:"read_people"`
	WritePeople   bool   `json:"write_people"`
	SaveSummaries bool   `json:"save_summaries"`
	DownloadMedia bool   `json:"download_media"`
	RequestSend   bool   `json:"request_send"`
}

// Preset defines the two owner-facing connection choices. Capability edits may further narrow either.
func Preset(kind string) Permissions {
	if kind == "given" {
		return Permissions{Access: Access{Circles: []string{}, Unsorted: false}, Chats: Scope{"all", []string{}}, ReadMessages: true, Search: true, ReadPeople: true}
	}
	return Permissions{Access: AllAccess(), Chats: Scope{"all", []string{}}, ReadMessages: true, Search: true, ReadPeople: true, WritePeople: true, SaveSummaries: true, DownloadMedia: true, RequestSend: true}
}

// Defaults is retained for reading legacy fixtures; new connections use Preset.
func Defaults(remote bool) Permissions {
	return Permissions{Access: AllAccess(), Chats: Scope{"all", []string{}}, ReadMessages: true, Search: true, ReadPeople: true, WritePeople: !remote, SaveSummaries: !remote, DownloadMedia: !remote, RequestSend: !remote}
}
func Contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (p Permissions) Allows(jid string, hidden []string) bool {
	if jid == "" || Contains(hidden, jid) {
		return false
	}
	switch p.Chats.Mode {
	case "all":
		return true
	case "only":
		return Contains(p.Chats.JIDs, jid)
	case "all_except":
		return !Contains(p.Chats.JIDs, jid)
	}
	return false
}
func (p Permissions) Valid() bool {
	if !Contains([]string{"all", "only", "all_except"}, p.Chats.Mode) {
		return false
	}
	return true
}

type Agent struct {
	Preset        string      `json:"preset,omitempty"`
	RunChat       string      `json:"-"`
	Access        Access      `json:"access"`
	AccessSummary string      `json:"access_summary"`
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Kind          string      `json:"kind"`
	Transport     string      `json:"transport"`
	CreatedAt     time.Time   `json:"created_at"`
	LastSeenAt    *time.Time  `json:"last_seen_at"`
	Enabled       bool        `json:"enabled"`
	Permissions   Permissions `json:"permissions"`
}
type Chat struct {
	JID             string     `json:"jid"`
	Name            string     `json:"name"`
	IsGroup         bool       `json:"is_group"`
	LastMessageTime *time.Time `json:"last_message_time"`
}
type MessageAuthor struct {
	Type      string `json:"type"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}
type Message struct {
	Author          MessageAuthor `json:"author"`
	Truncated       bool          `json:"truncated,omitempty"`
	FullLength      int           `json:"full_length,omitempty"`
	ID              string        `json:"id"`
	ChatJID         string        `json:"chat_jid"`
	Sender          string        `json:"sender,omitempty"`
	Content         string        `json:"content,omitempty"`
	Timestamp       time.Time     `json:"timestamp"`
	IsFromMe        bool          `json:"is_from_me"`
	MediaType       string        `json:"media_type,omitempty"`
	Filename        string        `json:"filename,omitempty"`
	QuotedMessageID string        `json:"quoted_message_id,omitempty"`
	DeletedAt       *time.Time    `json:"deleted_at,omitempty"`
}
type Attachment struct {
	MIME      string    `json:"mime_type,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
}
type Approval struct {
	AsDocument     bool        `json:"as_document,omitempty"`
	Status         string      `json:"status,omitempty"`
	SentAt         *time.Time  `json:"sent_at,omitempty"`
	DeliveredAt    *time.Time  `json:"delivered_at,omitempty"`
	ReadAt         *time.Time  `json:"read_at,omitempty"`
	DeliveredCount int         `json:"delivered_count"`
	ReadCount      int         `json:"read_count"`
	ID             string      `json:"id"`
	AgentID        string      `json:"agent_id"`
	ChatJID        string      `json:"chat_jid"`
	Kind           string      `json:"kind"`
	Text           string      `json:"text"`
	Attachment     *Attachment `json:"attachment,omitempty"`
	ReplyTo        string      `json:"reply_to,omitempty"`
	MessageID      string      `json:"message_id,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
	State          string      `json:"state"`
	Edited         bool        `json:"edited"`
	Reason         string      `json:"reason,omitempty"`
}
type Activity struct {
	CalledAs         string    `json:"called_as,omitempty"`
	OwnerInstruction string    `json:"owner_instruction,omitempty"`
	QueueForApproval bool      `json:"queue_for_approval,omitempty"`
	DraftedBy        string    `json:"drafted_by,omitempty"`
	Edited           *bool     `json:"edited,omitempty"`
	Variation        *int      `json:"variation,omitempty"`
	Description      string    `json:"description,omitempty"`
	ConfirmedVia     string    `json:"confirmed_via,omitempty"`
	ID               int64     `json:"id"`
	At               time.Time `json:"at"`
	AgentID          string    `json:"agent_id"`
	Tool             string    `json:"tool"`
	Kind             string    `json:"kind"`
	ChatJIDs         []string  `json:"chat_jids"`
	MessageIDs       []string  `json:"message_ids"`
	Count            int       `json:"count"`
	Query            string    `json:"query,omitempty"`
	Note             string    `json:"note,omitempty"`
}
type LabeledValue struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Source string `json:"source"`
}
type PhotoReference struct {
	Source    string    `json:"source"`
	SHA256    string    `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
}
type PersonDetails struct {
	Org      string            `json:"org"`
	Title    string            `json:"title"`
	Sources  map[string]string `json:"sources,omitempty"`
	Phones   []LabeledValue    `json:"phones"`
	Emails   []LabeledValue    `json:"emails"`
	Birthday string            `json:"birthday"`
	Handles  []LabeledValue    `json:"handles"`
	Location string            `json:"location"`
	Pronouns string            `json:"pronouns"`
}

// MarshalJSON always writes lists as arrays (never null), so clients can decode them without special cases.
func (d PersonDetails) MarshalJSON() ([]byte, error) {
	type plain PersonDetails
	for _, xs := range []*[]LabeledValue{&d.Phones, &d.Emails, &d.Handles} {
		if *xs == nil {
			*xs = []LabeledValue{}
		}
	}
	return json.Marshal(plain(d))
}

type DetailSuggestion struct {
	CreatedAt          time.Time `json:"created_at,omitempty"`
	EvidenceMessageIDs []string  `json:"evidence_message_ids,omitempty"`
	EvidenceChats      []string  `json:"evidence_chats,omitempty"`
	ID                 string    `json:"id"`
	Field              string    `json:"field"`
	Label              string    `json:"label"`
	Value              string    `json:"value"`
	Source             string    `json:"source"`
	Status             string    `json:"status"`
}
type Person struct {
	OtherNames        []string           `json:"other_names,omitempty"`
	Photo             *PhotoReference    `json:"photo,omitempty"`
	ContactsPhoto     *PhotoReference    `json:"contacts_photo,omitempty"`
	Business          bool               `json:"business,omitempty"`
	Broadcast         bool               `json:"broadcast,omitempty"`
	Details           PersonDetails      `json:"details"`
	DetailSuggestions []DetailSuggestion `json:"detail_suggestions,omitempty"`

	ID                   string                 `json:"id"`
	JIDs                 []string               `json:"jids"`
	Name                 string                 `json:"name"`
	PushName             string                 `json:"push_name,omitempty"`
	Label                string                 `json:"label,omitempty"`
	Circle               string                 `json:"circle,omitempty"`
	Role                 string                 `json:"role,omitempty"`
	HowKnown             string                 `json:"how_known,omitempty"`
	Notes                string                 `json:"notes,omitempty"`
	UpdatedAt            time.Time              `json:"updated_at"`
	CircleAffinity       []CircleAffinity       `json:"circle_affinity"`
	PrimaryCircleID      string                 `json:"primary_circle_id,omitempty"`
	InferredRelationship *RelationshipInference `json:"inferred_relationship,omitempty"`
	GroupsInCommon       []GroupInCommon        `json:"groups_in_common"`
}
type GroupAccess struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Read    bool   `json:"read"`
	Send    string `json:"send"`
}
type Group struct {
	LastActivity *time.Time    `json:"last_activity"`
	Messages30   int           `json:"messages_30"`
	CircleIDs    []string      `json:"circle_ids"`
	TaskIDs      []string      `json:"task_ids"`
	Muted        bool          `json:"muted"`
	Archived     bool          `json:"archived"`
	Photo        bool          `json:"photo"`
	AgentAccess  []GroupAccess `json:"agent_access"`
	IsCommunity  bool          `json:"is_community,omitempty"`
	ParentJID    string        `json:"parent_jid,omitempty"`
	JID          string        `json:"jid"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	MemberCount  int           `json:"member_count"`
	IAmAdmin     bool          `json:"i_am_admin"`
}
type Membership struct {
	Group  string `json:"group"`
	Person string `json:"person"`
	Admin  bool   `json:"admin"`
}
type Fact struct {
	ID                 string    `json:"id"`
	Subject            string    `json:"subject"`
	SubjectID          string    `json:"subject_id"`
	Text               string    `json:"text"`
	Source             string    `json:"source"`
	EvidenceMessageIDs []string  `json:"evidence_message_ids"`
	EvidenceChats      []string  `json:"evidence_chat_jids,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	Status             string    `json:"status"`
}
type Relation struct {
	EvidenceMessageIDs []string `json:"evidence_message_ids,omitempty"`
	EvidenceChats      []string `json:"evidence_chat_jids,omitempty"`
	ID                 string   `json:"id"`
	A                  string   `json:"a"`
	B                  string   `json:"b"`
	Kind               string   `json:"kind"`
	Note               string   `json:"note"`
	Source             string   `json:"source"`
}
type Summary struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	ChatJID   string    `json:"chat_jid"`
	Period    string    `json:"period"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}
type TaskQuietHours struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone,omitempty"`
}
type AccessoryCue struct {
	Enabled    bool            `json:"enabled"`
	Color      string          `json:"color,omitempty"`
	QuietHours *TaskQuietHours `json:"quiet_hours,omitempty"`
}
type Settings struct {
	AccessoryCue      AccessoryCue `json:"accessory_cue"`
	AutoCircles   *bool    `json:"auto_circles,omitempty"`
	MaxCircles    int      `json:"max_circles,omitempty"`
	CurrentDays   int      `json:"current_days"`
	HiddenChats   []string `json:"hidden_chats"`
	RetentionDays int      `json:"retention_days"`
	Owner         string   `json:"owner"`
}

func (s Settings) AutoCirclesEnabled() bool { return s.AutoCircles == nil || *s.AutoCircles }

type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	FixID  string `json:"fix_id,omitempty"`
}
