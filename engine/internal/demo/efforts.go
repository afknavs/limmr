package demo

import (
	"fmt"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"time"
)

func seedEfforts(h *hub.Service) error {
	var done bool
	if h.Repo.Get("demo_meta", "efforts_v2", &done) == nil {
		return nil
	}
	fixture := []struct {
		title, topic, status, summary string
		person, group                 int
	}{
		{"Pitch deck with Maya", "pitch slides", "on_you", "Maya finished the layout. Review the slides before Thursday.", 4, 3},
		{"Lab report", "experiment report", "waiting_on_them", "Your results are ready. Noor is checking the method section.", 5, 3},
		{"Weekend plan", "hiking route", "moving", "The route is taking shape. Confirm a meeting point next.", 6, 2},
		{"Garden supplies", "garden supplies", "dormant", "", 10, 1},
		{"Design Club poster", "poster typography", "done", "Maya submitted the final poster.", 11, 5},
	}
	svc := &efforts.Service{Hub: h}
	for i, f := range fixture {
		texts := []string{f.topic, f.topic, f.topic}
		for j, text := range texts {
			chat := DM(f.person)
			if j == 1 {
				chat = Group(f.group)
			}
			at := Epoch.Add(-time.Duration((3-j)*24+i) * time.Hour)
			if f.status == "dormant" {
				at = at.Add(-15 * 24 * time.Hour)
			}
			if e := h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("e1-%d-%d", i, j), ChatJID: chat, Sender: DM(f.person), Content: text, Timestamp: at}); e != nil {
				return e
			}
		}
		xs, e := svc.List(nil, efforts.Filter{Query: f.topic})
		if e != nil {
			return fmt.Errorf("effort fixture %d list: %w", i, e)
		}
		for _, x := range xs {
			found := false
			for _, r := range x.MessageRefs {
				if r.MessageID == fmt.Sprintf("e1-%d-0", i) {
					found = true
				}
			}
			if found {
				_, e = svc.Update(&model.Agent{ID: "agent-2", Name: "Codex", Permissions: model.Defaults(false)}, x.ID, efforts.Update{Title: &f.title, Summary: &f.summary, Status: &f.status})
				if e != nil {
					return fmt.Errorf("effort fixture %d update: %w", i, e)
				}
				break
			}
		}
	}
	// A valid purpose without a safe noun phrase stays available to the naming queue.
	for i, text := range []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"} {
		if e := h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("a6-budget-%d", i),
			ChatJID: DM(5), Sender: DM(5), Content: text, IsFromMe: i == 1,
			Timestamp: Epoch.Add(time.Duration(i-3) * time.Hour)}); e != nil {
			return e
		}
	}
	return h.Repo.Put("demo_meta", "efforts_v2", true)
}
