package hub_test

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/testkit"
	"testing"
)

func TestZ2ActivityRepairsHistoricalNullArrays(t *testing.T) {
	h, _, _ := testkit.Open(t)
	_, err := h.Repo.DB.Exec(`INSERT INTO activity(at,data) VALUES('2026-10-26T00:00:00Z','{"at":"2026-10-26T00:00:00Z","agent_id":"you","tool":"circle_to_task","kind":"people_write","chat_jids":null,"message_ids":null}')`)
	testkit.Must(t, err)
	_, err = h.Repo.DB.Exec(`INSERT INTO activity(at,data) VALUES('2026-10-26T01:00:00Z','{"message_ids":[42]}')`)
	testkit.Must(t, err)
	rows, err := h.Activity(nil, hub.ActivityFilter{})
	testkit.Must(t, err)
	if len(rows) != 1 || rows[0].ChatJIDs == nil || rows[0].MessageIDs == nil {
		t.Fatal("historical arrays were not normalized")
	}
	raw, err := json.Marshal(rows[0])
	testkit.Must(t, err)
	var fields map[string]any
	testkit.Must(t, json.Unmarshal(raw, &fields))
	if fields["chat_jids"] == nil || fields["message_ids"] == nil {
		t.Fatal("wire arrays must be empty, not null")
	}
}
