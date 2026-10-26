package integration_test

import (
	"context"
	"limmr/internal/model"
	"sync"
	"testing"
	"time"
)

func TestUncircledSendRequiresChatConfirmation(t *testing.T) {
	r := newRig(t)
	out, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Draft text", "voice": "as_agent"})
	must(t, e)
	d := out.(model.Draft)
	want := "Draft text"
	if d.State != "draft" || d.FinalText != want || d.Rule != "confirm" || len(r.fake.Sent) != 0 {
		t.Fatal("draft mismatch")
	}
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrConflict {
		t.Fatal("missing 2s guard")
	}
	r.h.Now = func() time.Time { return epoch.Add(2 * time.Second) }
	d, e = r.h.SendDraft(context.Background(), r.a, d.ID)
	must(t, e)
	if d.State != "sent" || d.ConfirmedVia != "chat" || len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != want {
		t.Fatal("wrong confirmed delivery")
	}
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrConflict {
		t.Fatal("replayed draft")
	}
}
func TestLegacyVoiceAcceptedBeforeConfirmation(t *testing.T) {
	r := newRig(t)
	out, err := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Draft text", "voice": "as_me", "signed": true})
	must(t, err)
	if out.(model.Draft).FinalText != "Draft text" || len(r.fake.Sent) != 0 {
		t.Fatal("legacy arguments changed text or skipped confirmation")
	}
}
func TestAsMeAndEditedApproval(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(map[bool]string{false: "as_me", true: "edited"}[edited], func(t *testing.T) {
			r := newRig(t)
			p := r.a.Permissions
			r.agent(t, p)
			circleFixture(t, r, "self", "free", true, chatA)
			ap, e := r.h.RequestSend(r.a, "message", chatA, "Original", "as_me", "", "")
			must(t, e)
			if ap.Text != "Original" {
				t.Fatal("as_me changed text")
			}
			var edit *string
			want := "Original"
			if edited {
				want = "Edited final text exactly"
				edit = &want
			}
			ap, e = r.h.Decide(context.Background(), ap.ID, true, edit, "")
			must(t, e)
			if ap.Edited != edited || r.fake.Sent[0].Text != want {
				t.Fatal("edit/signature mismatch")
			}
		})
	}
}
func TestDenyAndExpireNeverSend(t *testing.T) {
	for _, decision := range []string{"deny", "expire", "expire_on_approve"} {
		t.Run(decision, func(t *testing.T) {
			r := newRig(t)
			ap, e := r.h.RequestSend(r.a, "message", chatA, "Draft", "as_agent", "", "")
			must(t, e)
			switch decision {
			case "deny":
				ap, e = r.h.Decide(context.Background(), ap.ID, false, nil, "Not now")
				must(t, e)
				if ap.State != "denied" {
					t.Fatal(ap.State)
				}
			case "expire":
				r.h.Now = func() time.Time { return epoch.Add(30 * time.Minute) }
				must(t, r.h.Expire())
				must(t, r.h.Repo.Get("approvals", ap.ID, &ap))
				if ap.State != "expired" {
					t.Fatal(ap.State)
				}
			case "expire_on_approve":
				r.h.Now = func() time.Time { return epoch.Add(time.Hour) }
				_, e = r.h.Decide(context.Background(), ap.ID, true, nil, "")
				if e != model.ErrConflict {
					t.Fatal(e)
				}
			}
			if len(r.fake.Sent) != 0 {
				t.Fatal("unapproved send")
			}
		})
	}
}
func TestApproveRechecksPermission(t *testing.T) {
	r := newRig(t)
	ap, e := r.h.RequestSend(r.a, "message", chatA, "Draft", "as_agent", "", "")
	must(t, e)
	p := r.a.Permissions
	p.RequestSend = false
	r.agent(t, p)
	_, e = r.h.Decide(context.Background(), ap.ID, true, nil, "")
	if e != model.ErrForbidden || len(r.fake.Sent) != 0 {
		t.Fatal("stale permission accepted")
	}
}
func TestApprovalOwnership(t *testing.T) {
	r := newRig(t)
	other, _, e := r.h.CreateAgent("Another AI", "custom", "local")
	must(t, e)
	ap, e := r.h.RequestSend(other, "message", chatA, "Draft", "as_agent", "", "")
	must(t, e)
	_, e = r.call(t, "check_send", map[string]any{"approval_id": ap.ID})
	if e != model.ErrNotFound {
		t.Fatal("approval belongs to other agent")
	}
}
func TestFileAndReactionApproval(t *testing.T) {
	for _, kind := range []string{"file", "reaction"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			at, e := r.h.Attachment([]byte("invented sketch"), "sketch.txt")
			must(t, e)
			message := firstMessage(t, r, chatA)
			args := map[string]any{"recipient": chatA, "text": "Thanks", "voice": "as_agent"}
			if kind == "file" {
				args["attachment_id"] = at.ID
			} else {
				args["message_id"] = message.ID
			}
			out, e := r.call(t, "send_"+kind, args)
			must(t, e)
			if len(r.fake.Sent) != 0 {
				t.Fatal("tool sent")
			}
			id := out.(model.Draft).ID
			r.h.Now = func() time.Time { return epoch.Add(2 * time.Second) }
			ap, e := r.h.SendDraft(context.Background(), r.a, id)
			must(t, e)
			if ap.Kind != kind || len(r.fake.Sent) != 1 {
				t.Fatal("wrong delivery")
			}
			if kind == "file" && (ap.Attachment == nil || ap.Attachment.SHA256 != at.SHA256) {
				t.Fatal("attachment pin lost")
			}
		})
	}
}
func TestConcurrentApprovalSendsOnce(t *testing.T) {
	r := newRig(t)
	ap, e := r.h.RequestSend(r.a, "message", chatA, "Draft", "as_agent", "", "")
	must(t, e)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.h.Decide(context.Background(), ap.ID, true, nil, "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if err != model.ErrConflict {
			t.Fatal(err)
		}
	}
	if success != 1 || len(r.fake.Sent) != 1 {
		t.Fatal("duplicate concurrent delivery")
	}
}

type failMessenger struct{}

func (failMessenger) Send(context.Context, model.Approval) (string, error) {
	return "", model.ErrConflict
}
func TestFailedSendIsTerminal(t *testing.T) {
	r := newRig(t)
	r.h.Messenger = failMessenger{}
	ap, e := r.h.RequestSend(r.a, "message", chatA, "Draft", "as_agent", "", "")
	must(t, e)
	ap, e = r.h.Decide(context.Background(), ap.ID, true, nil, "")
	must(t, e)
	if ap.State != "failed" {
		t.Fatal(ap.State)
	}
	if _, e = r.h.Decide(context.Background(), ap.ID, true, nil, ""); e != model.ErrConflict {
		t.Fatal("retried failed send")
	}
}
