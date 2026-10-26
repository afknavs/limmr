package model

import (
	"encoding/json"
	"testing"
)

func TestAccessWireDefaultsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		all, unsorted bool
	}{
		{`{"chats":{"mode":"all"}}`, true, true},
		{`{"access":{}}`, true, true},
		{`{"access":{"circles":"all"}}`, true, true},
		{`{"access":{"circles":[]}}`, false, false},
		{`{"access":{"circles":["work"]}}`, false, false},
		{`{"access":{"circles":["work"],"unsorted":true}}`, false, true},
	} {
		var p Permissions
		if e := json.Unmarshal([]byte(tc.raw), &p); e != nil {
			t.Fatal(e)
		}
		if (p.Access.Circles == nil) != tc.all || p.Access.Unsorted != tc.unsorted {
			t.Fatal("legacy/default access changed")
		}
		b, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		var again Permissions
		if e = json.Unmarshal(b, &again); e != nil {
			t.Fatal(e)
		}
		if (again.Access.Circles == nil) != tc.all {
			t.Fatal("empty scope became everything")
		}
	}
	for _, raw := range []string{`null`, `{"circles":null}`, `{"circles":"work"}`, `{"circles":["work","work"]}`, `{"circles":[""]}`, `{"circles":42}`, `{"unsorted":null}`, `{"until":"tomorrow"}`, `{"extra":true}`} {
		var a Access
		if json.Unmarshal([]byte(raw), &a) == nil {
			t.Fatal("invalid access accepted")
		}
	}
}
