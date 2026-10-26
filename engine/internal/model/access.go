package model

import (
	"bytes"
	"encoding/json"
	"time"
)

// CircleSelection is nil for everything; a non-nil empty slice grants no circles.
type CircleSelection []string

func (c CircleSelection) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte(`"all"`), nil
	}
	return json.Marshal([]string(c))
}
func (c *CircleSelection) UnmarshalJSON(b []byte) error {
	if string(b) == `"all"` {
		*c = nil
		return nil
	}
	if len(b) == 0 || b[0] != '[' {
		return ErrInvalid
	}
	var ids []string
	if json.Unmarshal(b, &ids) != nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 200 || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	*c = append(CircleSelection{}, ids...)
	return nil
}

type Access struct {
	Circles  CircleSelection `json:"circles"`
	Unsorted bool            `json:"unsorted"`
	Until    *time.Time      `json:"until"`
}

func AllAccess() Access { return Access{Unsorted: true} }
func (a *Access) UnmarshalJSON(b []byte) error {
	type plain Access
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil || fields == nil {
		return ErrInvalid
	}
	for k, v := range fields {
		if k != "circles" && k != "unsorted" && k != "until" {
			return ErrInvalid
		}
		if k != "until" && bytes.Equal(v, []byte("null")) {
			return ErrInvalid
		}
	}
	v := plain(AllAccess())
	if json.Unmarshal(b, &v) != nil {
		return ErrInvalid
	}
	if _, ok := fields["unsorted"]; !ok {
		v.Unsorted = v.Circles == nil
	}
	*a = Access(v)
	return nil
}
func (a Access) Ended(now time.Time) bool    { return a.Until != nil && !now.Before(*a.Until) }
func (a Access) AllowsCircle(id string) bool { return a.Circles == nil || Contains(a.Circles, id) }

// Legacy permission JSON has no access field and retains everything.
func (p *Permissions) UnmarshalJSON(b []byte) error {
	type plain Permissions
	v := plain{Access: AllAccess()}
	var legacy map[string]json.RawMessage
	if json.Unmarshal(b, &legacy) != nil || legacy == nil {
		return ErrInvalid
	}
	legacySendAllowed := true
	if raw, ok := legacy["voices"]; ok {
		var voices []string
		legacySendAllowed = json.Unmarshal(raw, &voices) == nil && (Contains(voices, "as_agent") || Contains(voices, "as_me"))
	}
	for _, k := range []string{"voices", "signature", "signature_mode"} {
		delete(legacy, k)
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil {
		return ErrInvalid
	}
	if !legacySendAllowed {
		v.RequestSend = false
	}
	*p = Permissions(v)
	return nil
}

type AccessEndedError struct{ Message string }

func (e *AccessEndedError) Error() string { return e.Message }
func (e *AccessEndedError) Unwrap() error { return ErrForbidden }
