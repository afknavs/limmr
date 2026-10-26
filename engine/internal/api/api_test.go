package api

import (
	"encoding/json"
	"errors"
	"limmr/internal/model"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownTrailingAndMalformedJSON(t *testing.T) {
	for _, body := range []string{`{"confirm":true,"unknown":1}`, `{"confirm":true} {}`, `{"confirm":`, ``} {
		t.Run(body, func(t *testing.T) {
			var v struct {
				Confirm bool `json:"confirm"`
			}
			r := httptest.NewRequest("POST", "/", strings.NewReader(body))
			if decode(r, &v) != model.ErrInvalid {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
	var v struct {
		Confirm bool `json:"confirm"`
	}
	if decode(httptest.NewRequest("POST", "/", strings.NewReader(`{"confirm":true}`)), &v) != nil || !v.Confirm {
		t.Fatal("valid body rejected")
	}
}
func TestErrorsNeverReflectPrivateDetails(t *testing.T) {
	for _, e := range []error{model.ErrInvalid, model.ErrForbidden, model.ErrNotFound, model.ErrConflict, errors.New("CANARY_PRIVATE_ERROR")} {
		w := httptest.NewRecorder()
		failure(w, e)
		if strings.Contains(w.Body.String(), "CANARY") {
			t.Fatal("error reflected details")
		}
		var body struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code == "" || body.Error.Message == "" {
			t.Fatal("unstable error shape")
		}
	}
}

func TestEveryControlRouteIsDocumented(t *testing.T) {
	source, e := os.ReadFile("api.go")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := os.ReadFile("../../../docs/API.md")
	if e != nil {
		t.Fatal(e)
	}
	normalize := func(s string) string { return regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(s, "{id}") }
	documented := map[string]bool{}
	for _, match := range regexp.MustCompile("`((?:GET|POST|PATCH|PUT|DELETE) /v1/[^`? ]+)(?:\\?[^`]*)?`").FindAllStringSubmatch(string(doc), -1) {
		documented[normalize(match[1])] = true
	}
	extra, e := os.ReadFile("circles.go")
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, extra...)
	watchSource, e := os.ReadFile("watches.go")
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, watchSource...)
	onboardingSource, e := os.ReadFile("onboarding.go")
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, onboardingSource...)
	updatesSource, e := os.ReadFile("updates.go")
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, updatesSource...)
	for _, file := range []string{"stats.go", "u4.go", "people_understanding.go", "efforts.go", "tasks.go", "y45.go", "sync.go"} {
		extra, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source = append(source, extra...)
	}
	routes := regexp.MustCompile(`add\("([A-Z]+ /v1/[^\"]+)"`).FindAllStringSubmatch(string(source), -1)
	if len(routes) != 152 {
		t.Fatalf("control route count changed: %d", len(routes))
	}
	for _, route := range routes {
		if !documented[normalize(route[1])] {
			t.Errorf("undocumented route %s", route[1])
		}
	}
	for _, kind := range []string{"chats", "people", "topics"} {
		for _, method := range []string{"POST", "PATCH", "DELETE"} {
			path := method + " /v1/circles/{id}/" + kind
			if method != "POST" {
				path += "/{id}"
			}
			if !documented[path] {
				t.Errorf("missing collection mutation %s", path)
			}
		}
	}
	if len(documented) != len(routes)+9 {
		t.Fatal("docs advertise routes not in the control handler")
	}
}
