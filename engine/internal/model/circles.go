package model

import (
	"encoding/json"
	"time"
)

type Circle struct {
	ConfirmedBy   string         `json:"confirmed_by,omitempty"`
	Archived      bool           `json:"archived,omitempty"`
	Goal          string         `json:"goal,omitempty"`
	ReviewReason  string         `json:"review_reason,omitempty"`
	Origin        *Origin        `json:"origin,omitempty"`
	ConfirmOrigin *Origin        `json:"confirm_origin,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Symbol        string         `json:"symbol"`
	Description   string         `json:"description"`
	SendRule      string         `json:"send_rule"`
	Pinned        bool           `json:"pinned"`
	CreatedBy     string         `json:"created_by"`
	Confirmed     bool           `json:"confirmed"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Chats         []CircleChat   `json:"chats"`
	People        []CirclePerson `json:"people"`
	Topics        []Topic        `json:"topics"`
}
type CircleChat struct {
	Origin        *Origin   `json:"origin,omitempty"`
	ChatJID       string    `json:"chat_jid"`
	Role          string    `json:"role_in_circle"`
	IncludedSince time.Time `json:"included_since"`
}
type CirclePerson struct {
	Origin   *Origin `json:"origin,omitempty"`
	PersonID string  `json:"person_id"`
	Role     string  `json:"role"`
	Note     string  `json:"note"`
	Source   string  `json:"source"`
}
type Topic struct {
	Origin         *Origin   `json:"origin,omitempty"`
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Chats          []string  `json:"chats"`
	People         []string  `json:"people"`
	Keywords       []string  `json:"keywords"`
	LastActivityAt time.Time `json:"last_activity_at"`
}
type Upcoming struct {
	Date string `json:"date"`
	Text string `json:"text"`
}
type PeopleNote struct {
	PersonID     string `json:"person_id"`
	Contribution string `json:"contribution"`
}
type Brief struct {
	CircleID    string       `json:"circle_id"`
	Version     int          `json:"version"`
	Summary     string       `json:"summary"`
	WhatChanged string       `json:"what_changed"`
	OpenAsks    []OpenAsk    `json:"open_asks_for_you"`
	Upcoming    []Upcoming   `json:"upcoming"`
	PeopleNotes []PeopleNote `json:"people_notes"`
	Evidence    []string     `json:"evidence_message_ids"`
	ChatJIDs    []string     `json:"chat_jids"`
	WrittenBy   string       `json:"written_by"`
	WrittenAt   time.Time    `json:"written_at"`
	CoversUntil time.Time    `json:"covers_until"`
}
type OpenAsk struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	ChatJID string    `json:"chat_jid"`
	Chat    string    `json:"chat"`
	From    string    `json:"from"`
	At      time.Time `json:"at"`
}

// Older briefs and MCP callers supplied strings.
func (a *OpenAsk) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &a.Text)
	}
	type plain OpenAsk
	return json.Unmarshal(b, (*plain)(a))
}

type Suggestion struct {
	DraftSource string    `json:"draft_source,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
	Variations  []string  `json:"variations"`
	Body        string    `json:"body"`
	ID          string    `json:"id"`
	CircleID    string    `json:"circle_id"`
	Kind        string    `json:"kind"`
	ChatJID     string    `json:"chat_jid,omitempty"`
	DraftText   string    `json:"draft_text,omitempty"`
	Why         string    `json:"why"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	State       string    `json:"state"`
	DraftID     string    `json:"draft_id,omitempty"`
}
type Proposal struct {
	BatchID   string    `json:"batch_id"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
	Circle    Circle    `json:"circle"`
	Why       string    `json:"why"`
	Source    string    `json:"source"`
}
type BriefSettings struct {
	OnDeviceBriefs struct {
		Enabled bool `json:"enabled"`
	} `json:"on_device_briefs"`
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"`
}
type Draft struct {
	TaskID           string     `json:"task_id,omitempty"`
	AsDocument       bool       `json:"as_document,omitempty"`
	OwnerInstruction string     `json:"owner_instruction,omitempty"`
	QueueForApproval bool       `json:"queue_for_approval,omitempty"`
	Key              string     `json:"key,omitempty"`
	Status           string     `json:"status"`
	ApprovalID       string     `json:"approval_id,omitempty"`
	SentAt           *time.Time `json:"sent_at,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	ReadAt           *time.Time `json:"read_at,omitempty"`
	DeliveredCount   int        `json:"delivered_count"`
	ReadCount        int        `json:"read_count"`

	Variations        []string    `json:"variations,omitempty"`
	Edited            bool        `json:"edited"`
	Variation         int         `json:"variation"`
	DraftedBy         string      `json:"drafted_by,omitempty"`
	HandoffID         string      `json:"handoff_id,omitempty"`
	RunID             string      `json:"run_id,omitempty"`
	ID                string      `json:"draft_id"`
	AgentID           string      `json:"agent_id"`
	ChatJID           string      `json:"chat_jid"`
	FinalText         string      `json:"final_text"`
	Rule              string      `json:"rule"`
	NeedsConfirmation bool        `json:"needs_confirmation"`
	CreatedAt         time.Time   `json:"created_at"`
	ExpiresAt         time.Time   `json:"expires_at"`
	State             string      `json:"state"`
	ConfirmedVia      string      `json:"confirmed_via,omitempty"`
	MessageID         string      `json:"message_id,omitempty"`
	SuggestionID      string      `json:"suggestion_id,omitempty"`
	Kind              string      `json:"kind,omitempty"`
	ReplyTo           string      `json:"reply_to,omitempty"`
	Attachment        *Attachment `json:"attachment,omitempty"`
}

func DefaultSendRule(kind string) string {
	if Contains([]string{"self", "family", "friends", "project"}, kind) {
		return "free"
	}
	return "confirm"
}
