package model

import "time"

// Origin is recorded at the authenticated write, never inferred from a label.
// By stores an agent ID internally; map responses resolve its display name.
type Origin struct {
	By           string    `json:"by"`
	ByKind       string    `json:"by_kind"`
	At           time.Time `json:"at"`
	Evidence     int       `json:"evidence"`
	BriefVersion int       `json:"brief_version,omitempty"`
	ChangeID     string    `json:"change_id,omitempty"`
}

type UnderstandingChange struct {
	ID       string `json:"id"`
	CircleID string `json:"circle_id"`
	TargetID string `json:"target_id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Origin   Origin `json:"origin"`
}

// Affinity is a scoped, computed projection, never a replacement for owner membership.
type CircleAffinity struct {
	CircleID string   `json:"circle_id"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons"`
}
type RelationshipInference struct {
	Label      string  `json:"label"`
	HowKnown   string  `json:"how_known"`
	Confidence float64 `json:"confidence"`
	Sensitive  bool    `json:"sensitive"`
	Status     string  `json:"status"`
}

// GroupInCommon is membership metadata only. Circle IDs are projected to the reader.
type GroupInCommon struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	CircleIDs    []string   `json:"circle_ids"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}
