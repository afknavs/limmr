package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
)

func TestMutationRollsBackSerializationAndSQLFailures(t *testing.T) {
	for _, mode := range []string{"json", "sql"} {
		t.Run(mode, func(t *testing.T) {
			h, _, _ := testkit.Open(t)
			value := any(map[string]string{"state": "new"})
			if mode == "json" {
				value = make(chan int)
			} else {
				_, err := h.Repo.DB.Exec("CREATE TRIGGER reject_change BEFORE INSERT ON objects WHEN new.bucket='changes' BEGIN SELECT RAISE(ABORT,'fixture failure'); END")
				testkit.Must(t, err)
			}
			err := h.Repo.Apply(hub.Mutation{Bucket: "people", ID: "example", Value: model.Person{ID: "example"}}, hub.Mutation{Bucket: "changes", ID: "change", Value: value})
			if err == nil {
				t.Fatal("failed mutation accepted")
			}
			var p model.Person
			if h.Repo.Get("people", "example", &p) != model.ErrNotFound {
				t.Fatal("partial mutation committed")
			}
			testkit.Must(t, h.Repo.Put("probe", "after", true)) // rollback released the single connection
		})
	}
}
func TestCredentialRotationRevocationAndDisabledProfile(t *testing.T) {
	h, _, _ := testkit.Open(t)
	a, old, err := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, err)
	fresh, err := h.Rotate(a.ID)
	testkit.Must(t, err)
	if _, err = h.Authenticate(old); err == nil {
		t.Fatal("rotated credential accepted")
	}
	got, err := h.Authenticate(fresh)
	testkit.Must(t, err)
	if got.LastSeenAt == nil {
		t.Fatal("last seen not updated")
	}
	a.Enabled = false
	testkit.Must(t, h.UpdateAgent(a))
	if _, err = h.Authenticate(fresh); err == nil {
		t.Fatal("disabled profile accepted")
	}
	a.Enabled = true
	testkit.Must(t, h.UpdateAgent(a))
	testkit.Must(t, h.Revoke(a.ID))
	if _, err = h.Authenticate(fresh); err == nil {
		t.Fatal("revoked credential accepted")
	}
}
