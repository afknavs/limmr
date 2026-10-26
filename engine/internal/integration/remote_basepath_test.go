package integration_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"limmr/internal/remote"
)

// A Funnel mount like https://host/limmr strips the prefix before Limmr sees the request,
// while every advertised URL keeps it.
func TestRemoteUnderBasePath(t *testing.T) {
	_, s := remoteRig(t)
	must(t, s.Configure(remote.Config{Enabled: true, PublicURL: "https://limmr.example/limmr"}))
	get := func(path, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "https://limmr.example"+path, nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}
	var as map[string]any
	w := get("/.well-known/oauth-authorization-server", "")
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &as))
	if as["issuer"] != "https://limmr.example/limmr" || as["token_endpoint"] != "https://limmr.example/limmr/token" {
		t.Fatalf("metadata %v", as)
	}
	var prm map[string]any
	w = get("/.well-known/oauth-protected-resource/mcp", "")
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &prm))
	if prm["resource"] != "https://limmr.example/limmr/mcp" {
		t.Fatalf("resource %v", prm)
	}
	if w := get("/pair/status?tx=missing", "https://limmr.example"); w.Code == 403 {
		t.Fatal("same-origin request refused under a base path")
	}
	status(t, get("/pair/status?tx=missing", "https://evil.example"), 403)
	if s.Status().MCPURL != "https://limmr.example/limmr/mcp" {
		t.Fatal(s.Status().MCPURL)
	}
}
