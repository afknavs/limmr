package integration_test

import (
	"limmr/internal/model"
	"strings"
	"testing"
)

func TestU3StatsAndDigestRoutesRespectCredentialClassAndScope(t *testing.T) {
	r := newRig(t)
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Kind: "read", Tool: "list_messages", ChatJIDs: []string{chatA}, Count: 14}))
	for _, path := range []string{"/v1/stats", "/v1/activity/digests"} {
		status(t, r.request(t, "GET", path, "", nil, nil), 401)
		status(t, r.request(t, "GET", path, r.api.StatusToken, nil, nil), 403)
		status(t, r.request(t, "GET", path, r.api.ControlToken, nil, nil), 200)
	}
	status(t, r.request(t, "GET", "/v1/stats", r.token, nil, nil), 403)
	w := r.request(t, "GET", "/v1/activity/digests", r.token, nil, nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "14 messages") || strings.Contains(w.Body.String(), "CANARY_MESSAGE") {
		t.Fatal("digest missing aggregate or copied message text")
	}
}
