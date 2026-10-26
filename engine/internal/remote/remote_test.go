package remote

import (
	"limmr/internal/mcpserver"
	"limmr/internal/testkit"
	"strings"
	"testing"
)

func TestPKCEAndURLValidation(t *testing.T) {
	if shaChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal("RFC 7636 vector mismatch")
	}
	for _, v := range []string{"short", strings.Repeat("a", 129), strings.Repeat("a", 42) + "!"} {
		if validVerifier(v) {
			t.Fatal("invalid verifier accepted")
		}
	}
	for _, v := range []string{"http://public.example", "https://user:pass@limmr.example", "https://limmr.example/a/../b", "https://limmr.example/Limmr", "https://limmr.example/a%2Fb", "https://limmr.example?q=1", "https://limmr.example/#x"} {
		if _, e := ValidURL(v); e == nil {
			t.Fatal("invalid origin accepted")
		}
	}
	for _, v := range []string{"https://limmr.example/", "https://limmr.example/limmr", "https://limmr.example/limmr/"} {
		if _, e := ValidURL(v); e != nil {
			t.Fatal(v, e)
		}
	}
}
func TestCorruptPersistedRemoteSettingsFailClosed(t *testing.T) {
	h, p, _ := testkit.Open(t)
	_, e := h.Repo.DB.Exec("INSERT INTO objects VALUES('remote_settings','main','{\"enabled\":true,')")
	testkit.Must(t, e)
	s := New(h, &mcpserver.Service{Hub: h, People: p}, 18801)
	defer s.Close()
	if s.Configuration().Enabled {
		t.Fatal("partial invalid settings enabled remote")
	}
	testkit.Must(t, s.Start())
	if s.Status().Enabled {
		t.Fatal("disabled listener started")
	}
}
