package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateControlContract(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/v1/updates/capabilities", "/v1/updates/prepare", "/v1/updates/resume"} {
		method := "POST"
		if path == "/v1/updates/capabilities" {
			method = "GET"
		}
		for _, token := range []string{r.token, r.api.StatusToken} {
			status(t, r.request(t, method, path, token, nil, nil), 403)
		}
		status(t, r.request(t, method, path, "", nil, nil), 401)
	}
	ap, err := r.h.RequestSend(r.a, "message", chatA, "Review", "", "", "")
	must(t, err)
	w := r.request(t, "GET", "/v1/updates/capabilities", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if w.Body.String() != "{\"can_prepare\":true}\n" {
		t.Fatal("capabilities shape")
	}
	w = r.request(t, "POST", "/v1/updates/prepare", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var p hub.UpdatePause
	must(t, json.Unmarshal(w.Body.Bytes(), &p))
	if !p.Paused || p.ActiveSends != 0 {
		t.Fatal(p)
	}
	status(t, r.request(t, "POST", "/v1/updates/prepare", r.api.ControlToken, nil, nil), 409)
	status(t, r.request(t, "GET", "/v1/status", r.api.StatusToken, nil, nil), 200)
	w = r.request(t, "POST", "/v1/approvals/"+ap.ID+"/approve", r.api.ControlToken, map[string]any{}, nil)
	status(t, w, 503)
	if !bytes.Contains(w.Body.Bytes(), []byte(model.ErrUpdating.Error())) {
		t.Fatal("missing retry guidance")
	}
	status(t, r.request(t, "POST", "/v1/approvals/"+ap.ID+"/deny", r.api.ControlToken, map[string]any{}, nil), 200)
	_, err = r.call(t, "read", map[string]any{"mode": "chat", "chat_jid": chatA})
	must(t, err)
	for i := 0; i < 2; i++ {
		status(t, r.request(t, "POST", "/v1/updates/resume", r.api.ControlToken, nil, nil), 200)
	}
	restarted := hub.New(r.h.Repo, r.m, r.home)
	if restarted.CheckUpdating() != nil {
		t.Fatal("restart retained update lease")
	}
	if r.h.CheckUpdating() != nil || len(r.fake.Sent) != 0 {
		t.Fatal("resume sent or failed to unblock")
	}
	sent, err := r.h.DraftMessage(context.Background(), r.a, chatA, "After resume", "", true, hub.SendOptions{OwnerInstruction: "Send this now."})
	must(t, err)
	if sent.State != "sent" || len(r.fake.Sent) != 1 {
		t.Fatal("resume did not permit delivery")
	}
}

func TestUpdatePauseEveryOutboundEntry(t *testing.T) {
	for _, entry := range []string{"send", "send_message", "send_file", "send_reaction", "draft_message", "send_draft", "notify_me", "request_approval", "approve", "queued_suggestion", "background_job", "handoff", "keyed_send"} {
		t.Run(entry, func(t *testing.T) {
			r := newRig(t)
			n1Circle(t, r, chatA, "free", false)
			n1Owner(t, r)
			d, err := r.h.DraftMessage(context.Background(), r.a, chatA, "Prepared", "", false)
			must(t, err)
			ap, err := r.h.RequestSend(r.a, "message", chatA, "Prepared", "", "", "")
			must(t, err)
			queued, err := r.h.DraftMessage(hub.WithBackground(context.Background(), true), r.a, chatA, "Queued", "", true, hub.SendOptions{QueueForApproval: true})
			must(t, err)
			ctx := context.Background()
			if entry == "handoff" {
				s, c, requests := setupWatches(t, r)
				createHandoff(t, r, nil)
				ctx = runContext(t, r, wake(t, s, c, requests))
			}
			p, err := r.h.PrepareUpdate(context.Background())
			must(t, err)
			if !p.Paused {
				t.Fatal(p)
			}
			switch entry {
			case "request_approval":
				_, err = r.h.RequestSend(r.a, "message", chatA, "New", "", "", "")
			case "approve":
				_, err = r.h.Decide(ctx, ap.ID, true, nil, "")
			case "queued_suggestion":
				_, err = r.h.SendSuggestion(ctx, queued.SuggestionID, nil)
			case "background_job":
				_, err = r.h.DraftMessage(hub.WithBackground(ctx, true), r.a, chatA, "Job", "", true, hub.SendOptions{OwnerInstruction: "Send the update."})
			case "handoff":
				_, err = r.h.DraftMessage(ctx, r.a, chatA, "Handoff", "", true, hub.SendOptions{OwnerInstruction: "Send the update."})
			case "keyed_send":
				_, err = r.h.DraftSendKey(ctx, r.a, "message", chatA, "Keyed", "", "", "", "key")
			default:
				args := map[string]any{"recipient": chatA, "text": "Example"}
				switch entry {
				case "send":
					args = map[string]any{"chat": chatA, "text": "Example"}
				case "send_file":
					args["content_text"] = "Example file"
					args["filename"] = "example.txt"
				case "send_reaction":
					args["message_id"] = firstMessage(t, r, chatA).ID
				case "draft_message":
					args = map[string]any{"chat": chatA, "text": "Example", "send_now": true}
				case "send_draft":
					args = map[string]any{"draft_id": d.ID}
				case "notify_me":
					args = map[string]any{"text": "Example", "key": "note"}
				}
				_, err = r.call(t, entry, args)
			}
			if !errors.Is(err, model.ErrUpdating) {
				t.Fatalf("entry did not report update pause: %v", err)
			}
			if len(r.fake.Sent) != 0 {
				t.Fatal("paused send reached messenger")
			}
			var stored model.Approval
			must(t, r.h.Repo.Get("approvals", ap.ID, &stored))
			if stored.State != "pending" {
				t.Fatal("approval consumed")
			}
			var draft model.Draft
			must(t, r.h.Repo.Get("drafts", d.ID, &draft))
			if draft.State != "draft" {
				t.Fatal("draft consumed")
			}
			var suggestion model.Suggestion
			must(t, r.h.Repo.Get("suggestions", queued.SuggestionID, &suggestion))
			if suggestion.State != "pending" {
				t.Fatal("queue consumed")
			}
		})
	}
}

type updateBlockingMessenger struct {
	entered chan struct{}
	release chan struct{}
}

func (m updateBlockingMessenger) Send(context.Context, model.Approval) (string, error) {
	close(m.entered)
	<-m.release
	return "fake-update-send", nil
}
func TestUpdateHTTPDrainAndCancelledPrepare(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "cancel"}[cancelled], func(t *testing.T) {
			r := newRig(t)
			n1Circle(t, r, chatA, "free", false)
			m := updateBlockingMessenger{make(chan struct{}), make(chan struct{})}
			r.h.Messenger = m
			sent := make(chan error, 1)
			go func() { _, err := r.h.DraftMessage(context.Background(), r.a, chatA, "Example", "", true); sent <- err }()
			<-m.entered
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest("POST", "http://127.0.0.1:18800/v1/updates/prepare", nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+r.api.ControlToken)
			w := httptest.NewRecorder()
			finished := make(chan struct{})
			go func() { r.api.Handler().ServeHTTP(w, req); close(finished) }()
			deadline := time.Now().Add(time.Second)
			for r.h.CheckUpdating() == nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if r.h.CheckUpdating() != model.ErrUpdating {
				close(m.release)
				t.Fatal("pause never acquired")
			}
			// Fails promptly even though the first send still holds the hub lock.
			start := time.Now()
			_, err := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Later"})
			if err != model.ErrUpdating || time.Since(start) > time.Second {
				close(m.release)
				t.Fatal("paused MCP send hung", err)
			}
			select {
			case <-finished:
				close(m.release)
				t.Fatal("did not drain")
			default:
			}
			if cancelled {
				cancel()
			} else {
				close(m.release)
				must(t, <-sent)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				if cancelled {
					close(m.release)
				}
				t.Fatal("prepare hung")
			}
			status(t, w, 200)
			var p hub.UpdatePause
			must(t, json.Unmarshal(w.Body.Bytes(), &p))
			if cancelled {
				if p.Paused || p.ActiveSends != 1 || r.h.CheckUpdating() != nil {
					t.Fatal(p)
				}
				close(m.release)
				must(t, <-sent)
			} else if !p.Paused || p.ActiveSends != 0 {
				t.Fatal(p)
			}
		})
	}
}
