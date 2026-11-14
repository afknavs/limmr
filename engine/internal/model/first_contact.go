package model

// FirstContactInfo is a bounded provenance snapshot, never a full private profile.
type FirstContactInfo struct {
	FirstContact       bool           `json:"first_contact,omitempty"`
	FirstContactPerson *ContactPerson `json:"first_contact_person,omitempty"`
	FirstContactGroup  *ContactGroup  `json:"first_contact_group,omitempty"`
}
type ContactPerson struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Phone string `json:"phone,omitempty"`
	Role  string `json:"role,omitempty"`
}
type ContactGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
