package people_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestDirectoryRepeatKeepsTimestampAndSyncLog(t *testing.T) {
	h, p, _ := testkit.Open(t)
	original := model.Person{ID: "maya", Name: "Maya Rao", JIDs: []string{"15550100111@s.whatsapp.net"}, UpdatedAt: time.Now().UTC()}
	testkit.Must(t, p.PutPerson(original))
	var count int
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&count))
	for i := 0; i < 3; i++ {
		incoming := original
		incoming.UpdatedAt = incoming.UpdatedAt.Add(time.Hour)
		testkit.Must(t, p.PutPerson(incoming))
	}
	var after int
	var saved model.Person
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	testkit.Must(t, h.Repo.Get("people", original.ID, &saved))
	if after != count || !saved.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatal("unchanged directory person was rewritten")
	}
	original.UpdatedAt = original.UpdatedAt.Add(24 * time.Hour)
	testkit.Must(t, h.Repo.Apply(hub.Mutation{Bucket: "people", ID: original.ID, Value: original}))
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	if after != count {
		t.Fatal("atomic person path wrote a timestamp-only change")
	}
	// Contacts normalization also creates empty slices/maps that JSON omits.
	testkit.Must(t, p.ImportContacts(nil))
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	if after != count {
		t.Fatal("empty Contacts snapshot rewrote an unchanged person")
	}
}
