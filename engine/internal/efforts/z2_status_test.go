package efforts

import (
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestZ2OwnerStatusSurvivesRecomputeUntilNewMessage(t *testing.T) {
	for _, status := range []string{"moving", "done"} {
		t.Run(status, func(t *testing.T) {
			s, _ := seed(t)
			xs, err := s.List(nil, Filter{})
			testkit.Must(t, err)
			id := xs[0].ID
			_, err = s.Update(nil, id, Update{Status: &status})
			testkit.Must(t, err)
			s.cache = nil
			d, err := s.Get(nil, id)
			testkit.Must(t, err)
			if d.Status != status {
				t.Fatal("owner edit was lost on recompute")
			}
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "deck-new", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Coffee and sunshine", Timestamp: testkit.Epoch.Add(time.Second)}))
			s.Hub.Now = func() time.Time { return testkit.Epoch.Add(2 * time.Second) }
			d, err = s.Get(nil, id)
			testkit.Must(t, err)
			if d.Status != "on_you" {
				t.Fatal("new request did not reopen owner attention")
			}
		})
	}
}
