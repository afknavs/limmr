package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func TestA1DirectSendPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, rule, preset, instruction, state string
		background, queue                      bool
	}{
		{name: "owner confirm", rule: "confirm", instruction: "Send Maya the outline now.", state: "sent"},
		{name: "conversation confirm", rule: "confirm", state: "draft"},
		{name: "background owner", rule: "confirm", instruction: "Send Maya the outline now.", background: true, state: "sent"},
		{name: "background confirm", rule: "confirm", background: true, state: "suggested"},
		{name: "unattended free", rule: "free", background: true, state: "suggested"},
		{name: "explicit queue", rule: "free", instruction: "Prepare it for review.", queue: true, state: "suggested"},
		{name: "ask in Limmr", rule: "ask_in_limmr", instruction: "Send the outline.", state: "suggested"},
		{name: "given instruction ignored", rule: "confirm", preset: "given", instruction: "Send Maya the outline now.", state: "draft"},
		{name: "given background", rule: "confirm", preset: "given", instruction: "Send Maya the outline now.", background: true, state: "suggested"},
		{name: "given free unchanged", rule: "free", preset: "given", background: true, state: "sent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			circleFixture(t, r, "project", tc.rule, false, chatA)
			if tc.preset != "" {
				r.a.Preset = tc.preset
				must(t, r.h.UpdateAgent(r.a))
			}
			args := map[string]any{"recipient": chatA, "text": "The outline is ready.", "queue_for_approval": tc.queue}
			if tc.instruction != "" {
				args["owner_instruction"] = tc.instruction
			}
			out, e := r.mc.Call(hub.WithBackground(context.Background(), tc.background), r.a.ID, "send_message", args)
			must(t, e)
			d := out.(model.Draft)
			if d.State != tc.state {
				t.Fatalf("state %s, want %s", d.State, tc.state)
			}
			var stored model.Draft
			must(t, r.h.Repo.Get("drafts", d.ID, &stored))
			instruction := tc.instruction
			if tc.preset == "given" {
				instruction = ""
			}
			if stored.OwnerInstruction != instruction || stored.QueueForApproval != tc.queue {
				t.Fatal("audit not persisted")
			}
			suggestions, e := r.h.Suggestions()
			must(t, e)
			if (len(suggestions) == 1) != (tc.state == "suggested") {
				t.Fatal("incorrect queue")
			}
			if (len(r.fake.Sent) == 1) != (tc.state == "sent") {
				t.Fatal("incorrect fake delivery")
			}
			acts, e := r.h.Activity(nil, hub.ActivityFilter{Agent: r.a.ID})
			must(t, e)
			if len(acts) == 0 || acts[0].OwnerInstruction != instruction {
				t.Fatal("request audit missing")
			}
			if tc.state == "sent" {
				sent, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "sent"})
				must(t, e)
				if len(sent) != 1 || sent[0].OwnerInstruction != instruction || sent[0].Note != d.FinalText {
					t.Fatal("delivery audit missing")
				}
			}
			if tc.state == "suggested" {
				// MCP cannot turn an opted-in queue into a direct send.
				_, e = r.h.SendDraft(context.Background(), r.a, d.ID, hub.SendOptions{OwnerInstruction: "Send now."})
				if e != model.ErrConflict {
					t.Fatal("parked draft escaped owner review")
				}
				edit := "The edited outline is ready."
				sent, e := r.h.SendSuggestion(context.Background(), d.SuggestionID, &edit)
				must(t, e)
				if sent.State != "sent" || sent.FinalText != edit || sent.ConfirmedVia != "mac" {
					t.Fatal("owner queue action failed")
				}
			}
		})
	}
}

func TestA1AllSendToolsAndConversationConsent(t *testing.T) {
	for _, tool := range []string{"send_message", "send_file", "send_reaction", "draft_message"} {
		t.Run(tool, func(t *testing.T) {
			r := newRig(t)
			args := map[string]any{"recipient": chatA, "text": "Exact text", "owner_instruction": "Send this to Maya now."}
			if tool == "draft_message" {
				delete(args, "recipient")
				args["chat"] = chatA
			}
			if tool == "send_file" {
				args["content_text"] = "Synthetic document"
				args["filename"] = "outline.txt"
			}
			if tool == "send_reaction" {
				args["message_id"] = dbutil.Digest(chatA)[:8] + "a"
			}
			out, e := r.call(t, tool, args)
			must(t, e)
			d := out.(model.Draft)
			if d.State != "sent" || d.ConfirmedVia != "owner_instruction" || d.NeedsConfirmation || len(r.fake.Sent) != 1 {
				t.Fatal("tool did not deliver owner instruction")
			}
		})
	}
	r := newRig(t)
	out, e := r.call(t, "draft_message", map[string]any{"chat": chatA, "text": "Please review."})
	must(t, e)
	d := out.(model.Draft)
	// A quoted conversational confirmation needs no artificial wait.
	out, e = r.call(t, "send_draft", map[string]any{"draft_id": d.ID, "owner_instruction": "Yes, send it."})
	must(t, e)
	if out.(model.Draft).State != "sent" || out.(model.Draft).OwnerInstruction != "Yes, send it." {
		t.Fatal("conversation confirmation failed")
	}
	// Without a quote the existing conversation-consent delay remains supported.
	out, e = r.call(t, "draft_message", map[string]any{"chat": chatA, "text": "Another draft."})
	must(t, e)
	d = out.(model.Draft)
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	out, e = r.call(t, "send_draft", map[string]any{"draft_id": d.ID})
	must(t, e)
	if out.(model.Draft).ConfirmedVia != "chat" {
		t.Fatal("conversation confirmation missing")
	}
}

func TestA1InstructionValidationAndBoundaries(t *testing.T) {
	for _, input := range []any{"", "  ", strings.Repeat("界", 501), false, 42} {
		r := newRig(t)
		_, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Exact", "owner_instruction": input})
		if !errors.Is(e, model.ErrInvalid) || len(r.fake.Sent) != 0 {
			t.Fatal("invalid instruction accepted")
		}
	}
	for _, mode := range []string{"never", "hidden", "out of scope", "disabled", "permission"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			switch mode {
			case "never":
				circleFixture(t, r, "project", "never", false, chatA)
			case "hidden":
				settings := r.h.Settings()
				settings.HiddenChats = []string{chatA}
				must(t, r.h.SetSettings(settings))
			case "out of scope":
				p := r.a.Permissions
				p.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
				r.agent(t, p)
			case "disabled":
				r.a.Enabled = false
				must(t, r.h.UpdateAgent(r.a))
			case "permission":
				p := r.a.Permissions
				p.RequestSend = false
				r.agent(t, p)
			}
			_, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Exact", "owner_instruction": "Send this now."})
			if e == nil || len(r.fake.Sent) != 0 {
				t.Fatal("instruction bypassed access")
			}
		})
	}
}

func TestA1CurrentPresetDedupeAndReviewPrecedence(t *testing.T) {
	r := newRig(t)
	circleFixture(t, r, "project", "free", false, chatA)
	review := circleFixture(t, r, "work", "ask_in_limmr", false, chatA)
	circleFixture(t, r, "university", "confirm", false, chatA)
	rule, e := r.h.SendPolicy(chatA)
	must(t, e)
	if rule != "ask_in_limmr" {
		t.Fatal("explicit review must beat conversation confirm")
	}
	// Removing explicit review restores conversation confirm without rewriting it.
	review.Confirmed = false
	must(t, r.h.Repo.Put("circles", review.ID, review))
	rule, e = r.h.SendPolicy(chatA)
	must(t, e)
	if rule != "confirm" {
		t.Fatal("conversation confirmation changed")
	}
	out, e := r.call(t, "draft_message", map[string]any{"chat": chatA, "text": "An exact draft."})
	must(t, e)
	d := out.(model.Draft)
	r.a.Preset = "given"
	must(t, r.h.UpdateAgent(r.a))
	_, e = r.call(t, "send_draft", map[string]any{"draft_id": d.ID, "owner_instruction": "Yes, send it."})
	if e != model.ErrConflict || len(r.fake.Sent) != 0 {
		t.Fatal("given preset bypassed conversation delay")
	}
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	out, e = r.call(t, "send_draft", map[string]any{"draft_id": d.ID, "owner_instruction": "Yes, send it."})
	must(t, e)
	if out.(model.Draft).OwnerInstruction != "" || out.(model.Draft).ConfirmedVia != "chat" {
		t.Fatal("given preset trusted instruction")
	}
	r.a.Preset = "own"
	must(t, r.h.UpdateAgent(r.a))
	args := map[string]any{"recipient": chatA, "text": "Send exactly once.", "owner_instruction": strings.Repeat("界", 500), "key": "a1-once"}
	out, e = r.call(t, "send_message", args)
	must(t, e)
	first := out.(model.Draft)
	out, e = r.call(t, "send_message", args)
	must(t, e)
	if first.State != "sent" || out.(model.Draft).ID != first.ID || len(r.fake.Sent) != 2 {
		t.Fatal("instructed send lost dedupe")
	}
	r.a.Name = "limmr-nightly"
	must(t, r.h.UpdateAgent(r.a))
	out, e = r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Unattended draft."})
	must(t, e)
	if out.(model.Draft).State != "suggested" {
		t.Fatal("nightly did not park")
	}
}

func TestA1BackgroundHeaderDeliversOwnerInstruction(t *testing.T) {
	r := newRig(t)
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "send_message", "arguments": map[string]any{"recipient": chatA, "text": "Exact background text.", "owner_instruction": "Send this while I am away."}}}
	response := r.request(t, "POST", "/mcp", r.token, body, map[string]string{"X-Limmr-Background": "1", "Accept": "application/json, text/event-stream"})
	status(t, response, 200)
	var result struct {
		Result struct {
			Structured struct {
				Data model.Draft `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	must(t, json.Unmarshal(response.Body.Bytes(), &result))
	d := result.Result.Structured.Data
	if d.State != "sent" || d.OwnerInstruction != "Send this while I am away." || len(r.fake.Sent) != 1 {
		t.Fatal("background transport lost owner instruction")
	}
}
