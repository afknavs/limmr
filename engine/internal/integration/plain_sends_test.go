package integration_test

import (
	"context"
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"testing"
	"time"
)

func TestPlainSendPathsAcceptLegacyArguments(t *testing.T) {
	for _, name := range []string{"send_message", "send_file", "send_reaction", "notify_me", "draft_message", "send_draft"} {
		for _, voice := range []string{"", "auto", "as_agent", "as_me", "cached-future-value"} {
			for _, signed := range []bool{true, false} {
				t.Run(name+"/"+voice+"/"+map[bool]string{true: "true", false: "false"}[signed], func(t *testing.T) {
					r := newRig(t)
					own := n1Owner(t, r)
					n1Circle(t, r, chatA, "free", false)
					n1Circle(t, r, own, "free", false)
					text := "Hello\n— A closing written by the caller\n"
					args := map[string]any{"voice": voice, "signed": signed, "text": text}
					switch name {
					case "notify_me":
						args["key"] = "plain-note"
					case "send_file":
						args["recipient"] = chatA
						args["content_text"] = "Invented file"
						args["filename"] = "example.txt"
					case "send_reaction":
						args["recipient"] = chatA
						args["message_id"] = firstMessage(t, r, chatA).ID
						args["text"] = "👍"
						text = "👍"
					case "draft_message":
						args["chat"] = chatA
						args["send_now"] = true
					case "send_draft":
						out, err := r.call(t, "draft_message", map[string]any{"chat": chatA, "text": text, "voice": voice, "signed": signed})
						must(t, err)
						args = map[string]any{"draft_id": out.(model.Draft).ID, "voice": voice, "signed": signed}
					default:
						args["recipient"] = chatA
					}
					out, err := r.call(t, name, args)
					must(t, err)
					d := out.(model.Draft)
					if d.State != "sent" || d.FinalText != text || len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != text || r.fake.Sent[0].AgentID != r.a.ID {
						t.Fatal("payload or agent attribution changed")
					}
					wire, err := json.Marshal(d)
					must(t, err)
					var payload map[string]any
					must(t, json.Unmarshal(wire, &payload))
					for _, key := range []string{"voice", "signed", "voice_used", "voice_reason"} {
						if _, ok := payload[key]; ok {
							t.Fatal("obsolete field returned")
						}
					}
					again, err := r.call(t, "get_send_status", map[string]any{"message_id": d.MessageID})
					must(t, err)
					if again.(model.Draft).AgentID != r.a.ID {
						t.Fatal("receipt attribution lost")
					}
				})
			}
		}
	}
}
func TestPlainLegacyApprovalAndBackgroundSuggestion(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "confirm", false)
	ap, err := r.h.RequestSend(r.a, "message", chatA, "Exact caption", "as_agent", "", "")
	must(t, err)
	if ap.Text != "Exact caption" {
		t.Fatal("approval modified")
	}
	_, err = r.h.Decide(context.Background(), ap.ID, true, nil, "")
	must(t, err)
	out, err := r.mc.Call(hub.WithBackground(context.Background(), true), r.a.ID, "draft_message", map[string]any{"chat": chatA, "text": "Background body", "voice": "as_me", "signed": true})
	must(t, err)
	d := out.(model.Draft)
	r.h.Now = func() time.Time { return epoch.Add(time.Second) }
	_, err = r.h.SendSuggestion(context.Background(), d.SuggestionID, nil)
	must(t, err)
	if len(r.fake.Sent) != 2 || r.fake.Sent[1].Text != "Background body" || r.fake.Sent[1].AgentID != r.a.ID {
		t.Fatal("background send modified")
	}
}
