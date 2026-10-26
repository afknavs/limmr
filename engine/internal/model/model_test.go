package model

import (
	"strings"
	"testing"
)

func TestScopeModesAndGlobalHidden(t *testing.T) {
	for _, mode := range []string{"all", "only", "all_except", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			p := Defaults(false)
			p.Chats = Scope{Mode: mode, JIDs: []string{"a"}}
			if p.Allows("", nil) || p.Allows("a", []string{"a"}) {
				t.Fatal("hidden/empty chat visible")
			}
			if p.Allows("a", nil) != (mode == "all" || mode == "only") || p.Allows("b", nil) != (mode == "all" || mode == "all_except") {
				t.Fatal("scope decision")
			}
		})
	}
}
func TestRemoteDefaultsAndOpaquePersonID(t *testing.T) {
	p := Defaults(true)
	if !p.Valid() || !p.ReadMessages || !p.Search || !p.ReadPeople || p.WritePeople || p.RequestSend || p.DownloadMedia || p.SaveSummaries {
		t.Fatal("unsafe remote defaults")
	}
	p.Chats.Mode = "invalid"
	if p.Valid() {
		t.Fatal("invalid scope")
	}
	a := PersonID("15550100001@s.whatsapp.net")
	if strings.Contains(a, "15550100001") || a == PersonID("900000001@lid") || a != PersonID("15550100001@s.whatsapp.net") {
		t.Fatal("person ID unstable or revealing")
	}
}
