package integration_test

import (
	"encoding/base64"
	"errors"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func TestH2OwnerAliasesOnReadToolsRespectScope(t *testing.T) {
	for _, tool := range []string{"get_chat", "list_messages", "get_message_context", "search_messages"} {
		for _, alias := range []string{"me", "myself", " ME "} {
			t.Run(tool+"/"+alias, func(t *testing.T) {
				r := newRig(t)
				own := n1Owner(t, r)
				must(t, r.m.PutMessage(model.Message{ID: "self-note", ChatJID: own, Sender: own, Content: "Synthetic personal reminder", Timestamp: epoch}))
				args := map[string]any{"chat_jid": alias}
				if tool == "get_message_context" {
					args["message_id"] = "self-note"
				}
				if tool == "search_messages" {
					args["query"] = "reminder"
				}
				out, err := r.call(t, tool, args)
				must(t, err)
				if !strings.Contains(encode(out), own) || strings.Contains(encode(out), "CANARY_MESSAGE") {
					t.Fatal("alias did not scope to owner")
				}
				// Resolved aliases must remain invisible when the self-chat is hidden.
				settings := r.h.Settings()
				settings.HiddenChats = []string{own}
				must(t, r.h.SetSettings(settings))
				if _, err = r.call(t, tool, args); !errors.Is(err, model.ErrNotFound) {
					t.Fatalf("hidden alias accepted: %v", err)
				}
				settings.HiddenChats = nil
				must(t, r.h.SetSettings(settings))
				permissions := r.a.Permissions
				permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
				r.agent(t, permissions)
				if _, err = r.call(t, tool, args); !errors.Is(err, model.ErrNotFound) {
					t.Fatalf("scope bypassed: %v", err)
				}
			})
		}
	}
}
func TestH2UnknownOwnerAliasCannotBecomeUnscopedSearch(t *testing.T) {
	r := newRig(t)
	for _, tool := range []string{"search_messages", "list_messages", "get_chat", "get_message_context"} {
		args := map[string]any{"chat_jid": "me"}
		if tool == "search_messages" {
			args["query"] = "Saturday"
		}
		if tool == "get_message_context" {
			args["message_id"] = "missing"
		}
		if _, err := r.call(t, tool, args); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("%s widened unknown owner: %v", tool, err)
		}
	}
}
func TestH2MediaDocumentOptionPersistsThroughFakeDelivery(t *testing.T) {
	for _, tool := range []string{"send_file", "notify_me"} {
		for _, asDocument := range []bool{false, true} {
			t.Run(tool+map[bool]string{true: "-document", false: "-photo"}[asDocument], func(t *testing.T) {
				r := newRig(t)
				own := n1Owner(t, r)
				n1Circle(t, r, chatA, "free", true)
				args := map[string]any{"text": "A synthetic photo", "content_base64": base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nsynthetic")), "filename": "photo.png", "as_document": asDocument, "key": "h2-photo"}
				if tool == "send_file" {
					args["recipient"] = chatA
				}
				out, err := r.call(t, tool, args)
				d := n1Draft(t, out, err)
				if d.State != "sent" || d.AsDocument != asDocument || len(r.fake.Sent) != 1 || r.fake.Sent[0].AsDocument != asDocument || r.fake.Sent[0].Attachment.MIME != "image/png" || r.fake.Sent[0].Text != args["text"] {
					t.Fatal("lost media delivery option")
				}
				if tool == "notify_me" && d.ChatJID != own {
					t.Fatal("wrong self recipient")
				}
				stored, err := r.h.SendStatus(r.a, "", d.ID, "")
				must(t, err)
				if stored.AsDocument != asDocument {
					t.Fatal("option did not survive persistence")
				}
				_, err = r.call(t, tool, args)
				must(t, err)
				if len(r.fake.Sent) != 1 {
					t.Fatal("retry duplicated media")
				}
			})
		}
	}
}

func TestH2DocumentOverrideSurvivesConfirmation(t *testing.T) {
	r := newRig(t)
	out, err := r.call(t, "send_file", map[string]any{"recipient": chatA, "content_text": "Synthetic notes", "filename": "notes.txt", "as_document": true})
	d := n1Draft(t, out, err)
	if d.State != "draft" || len(r.fake.Sent) != 0 {
		t.Fatal("confirmation was bypassed")
	}
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	out, err = r.call(t, "send_draft", map[string]any{"draft_id": d.ID})
	sent := n1Draft(t, out, err)
	if sent.State != "sent" || len(r.fake.Sent) != 1 || !r.fake.Sent[0].AsDocument {
		t.Fatal("document override lost during confirmation")
	}
}
