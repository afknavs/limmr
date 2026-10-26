package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/wa"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func n1Circle(t *testing.T, r *rig, chat, rule string, asMe bool) {
	t.Helper()
	must(t, r.h.Repo.Put("circles", chat, model.Circle{ID: chat, Name: "Friends", Confirmed: true, Kind: "friends", SendRule: rule, Chats: []model.CircleChat{{ChatJID: chat}}}))
}
func n1Owner(t *testing.T, r *rig) string {
	t.Helper()
	const own = "15550100999@s.whatsapp.net"
	must(t, r.m.PutChat(model.Chat{JID: own, Name: "You"}))
	must(t, r.p.KeepOwner(model.Person{ID: "me", JIDs: []string{own}, Name: "Demo owner"}))
	return own
}
func n1Draft(t *testing.T, out any, e error) model.Draft {
	t.Helper()
	must(t, e)
	return out.(model.Draft)
}
func TestN1StableToolsPermissionError(t *testing.T) {
	r := newRig(t)
	r.agent(t, model.Defaults(true))
	listed := false
	for _, tool := range r.mc.Tools(r.a) {
		if tool.Name == "send" {
			listed = true
			if !strings.Contains(tool.Description, "needs: sending") {
				t.Fatal("missing switch hint")
			}
		}
	}
	if !listed {
		t.Fatal("sending hidden")
	}
	_, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "hello"})
	if e == nil || !strings.Contains(e.Error(), "Sending is off for Example AI.") || !strings.Contains(e.Error(), "Limmr › Integrations › Example AI") {
		t.Fatalf("not actionable: %v", e)
	}
	ds, e := hub.List[model.Draft](r.h.Repo, "drafts")
	must(t, e)
	if len(ds) != 0 || len(r.fake.Sent) != 0 {
		t.Fatal("denied send left draft")
	}
	p := r.a.Permissions
	p.RequestSend = true
	r.agent(t, p)
	out, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "hello"})
	d := n1Draft(t, out, e)
	if d.Status != "queued" || d.ApprovalID != d.ID {
		t.Fatal("sending change unavailable")
	}
}
func TestN1NotifyMeAndPolicy(t *testing.T) {
	for _, rule := range []string{"free", "confirm", "never"} {
		t.Run(rule, func(t *testing.T) {
			r := newRig(t)
			own := n1Owner(t, r)
			var c model.Circle
			must(t, r.h.Repo.Get("circles", "you", &c))
			c.SendRule = rule
			must(t, r.h.Repo.Put("circles", "you", c))
			out, e := r.call(t, "notify_me", map[string]any{"text": "Your notes are ready", "key": "daily-notes", "urgent": true})
			if rule == "never" {
				if e == nil || len(r.fake.Sent) != 0 {
					t.Fatal("never bypassed")
				}
				return
			}
			d := n1Draft(t, out, e)
			if d.ChatJID != own || d.FinalText != "Your notes are ready" {
				t.Fatal("owner/signature wrong")
			}
			want := 0
			if rule == "free" {
				want = 1
			}
			if len(r.fake.Sent) != want {
				t.Fatal("circle rule bypassed")
			}
		})
	}
	r := newRig(t)
	if _, e := r.call(t, "notify_me", map[string]any{"text": "hello"}); e == nil {
		t.Fatal("invented owner destination")
	}
}
func TestN1InlineFilesUploadReuseAndOwnership(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "free", true)
	for _, args := range []map[string]any{{"content_text": "notes π", "filename": "../notes.md"}, {"content_base64": base64.StdEncoding.EncodeToString([]byte{0, 1, 255}), "filename": "binary.dat"}} {
		args["recipient"] = chatA
		out, e := r.call(t, "send_file", args)
		d := n1Draft(t, out, e)
		if d.Attachment == nil || d.Attachment.Size == 0 || d.Attachment.MIME == "" || strings.Contains(d.Attachment.Name, "/") {
			t.Fatal("file metadata")
		}
	}
	out, e := r.call(t, "upload_attachment", map[string]any{"content_text": "reusable", "filename": "a.txt"})
	must(t, e)
	at := out.(map[string]any)["attachment_id"].(string)
	out, e = r.call(t, "send_file", map[string]any{"recipient": chatA, "attachment_id": at, "key": "file-1"})
	d := n1Draft(t, out, e)
	// An expired file URL on retry is never fetched; the persisted result wins.
	out, e = r.call(t, "send_file", map[string]any{"recipient": chatA, "url": "https://127.0.0.1/x", "key": "file-1"})
	again := n1Draft(t, out, e)
	if again.MessageID != d.MessageID || len(r.fake.Sent) != 3 {
		t.Fatal("file retry duplicated")
	}
	other, _, e := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, e)
	_, e = r.mc.Call(context.Background(), other.ID, "send_file", map[string]any{"recipient": chatA, "attachment_id": at})
	if e != model.ErrNotFound {
		t.Fatal("another AI reused upload")
	}
	_, e = r.call(t, "send_file", map[string]any{"recipient": chatA, "content_text": "x", "content_base64": "eA==", "filename": "x"})
	if e == nil {
		t.Fatal("multiple sources accepted")
	}
	n1Owner(t, r)
	out, e = r.call(t, "notify_me", map[string]any{"text": "File ready", "content_text": "hello", "filename": "hello.txt", "key": "notify-file"})
	d = n1Draft(t, out, e)
	if d.Kind != "file" || d.Status != "sent" {
		t.Fatal("notification file failed")
	}
}
func TestN1KeyPersistenceConcurrencyAndExpiry(t *testing.T) {
	for _, kind := range []string{"send_message", "send_file", "send_reaction", "notify_me"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			n1Circle(t, r, chatA, "free", true)
			n1Owner(t, r)
			args := map[string]any{"recipient": chatA, "text": "hello", "key": "same-key"}
			if kind == "send_file" {
				args["content_text"] = "hello"
				args["filename"] = "a.txt"
			}
			if kind == "send_reaction" {
				args["message_id"] = firstMessage(t, r, chatA).ID
				args["text"] = "👍"
			}
			if kind == "notify_me" {
				delete(args, "recipient")
			}
			out, e := r.call(t, kind, args)
			first := n1Draft(t, out, e)
			must(t, r.h.Repo.Close())
			repo, e := hub.Open(filepath.Join(r.home, "hub.db"))
			must(t, e)
			defer repo.Close()
			r.h = hub.New(repo, r.m, r.home)
			r.h.Now = func() time.Time { return epoch }
			r.h.Messenger = r.fake
			r.p.Hub = r.h
			r.mc.Hub = r.h
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					out, e := r.call(t, kind, args)
					if e != nil {
						t.Error(e)
						return
					}
					d := out.(model.Draft)
					if d.ID != first.ID || d.MessageID != first.MessageID || d.Status != first.Status {
						t.Error("key outcome changed")
					}
				}()
			}
			wg.Wait()
			if len(r.fake.Sent) != 1 {
				t.Fatal("duplicate fake delivery")
			}
			r.h.Now = func() time.Time { return epoch.Add(25 * time.Hour) }
			_, e = r.call(t, kind, args)
			must(t, e)
			if len(r.fake.Sent) != 2 {
				t.Fatal("key did not expire")
			}
		})
	}
	r := newRig(t)
	_, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "x", "key": strings.Repeat("x", 129)})
	if e == nil {
		t.Fatal("long key")
	}
}
func TestN1KeysDoNotCrossAgentsOrRevokedScopes(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "free", true)
	args := map[string]any{"recipient": chatA, "text": "hello", "key": "one"}
	_, e := r.call(t, "send_message", args)
	must(t, e)
	other, _, e := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, e)
	_, e = r.mc.Call(context.Background(), other.ID, "send_message", args)
	must(t, e)
	if len(r.fake.Sent) != 2 {
		t.Fatal("keys crossed AI identities")
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
	r.agent(t, p)
	if _, e = r.call(t, "send_message", args); e == nil {
		t.Fatal("saved key leaked revoked chat")
	}
}
func TestN1LegacyVoiceHasNoAutoRules(t *testing.T) {
	for _, chat := range []string{chatA, groupA, "me"} {
		for _, voice := range []string{"", "auto", "as_me", "as_agent", "unknown"} {
			t.Run(chat+voice, func(t *testing.T) {
				r := newRig(t)
				own := n1Owner(t, r)
				target := chat
				if target == "me" {
					target = own
				}
				n1Circle(t, r, target, "free", false)
				out, err := r.call(t, "send_message", map[string]any{"recipient": chat, "text": "hello", "voice": voice, "signed": true})
				d := n1Draft(t, out, err)
				if d.FinalText != "hello" || len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != "hello" {
					t.Fatal("legacy voice changed payload")
				}
				acts, err := r.h.Activity(&r.a, hub.ActivityFilter{Kind: "sent"})
				must(t, err)
				if len(acts) != 1 || acts[0].AgentID != r.a.ID {
					t.Fatal("attribution missing")
				}
			})
		}
	}
}
func TestN1ReceiptsAndStatus(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, groupA, "free", true)
	out, e := r.call(t, "send_message", map[string]any{"recipient": groupA, "text": "hello", "key": "receipt"})
	d := n1Draft(t, out, e)
	must(t, r.h.RecordReceipt(groupA, chatA, []string{d.MessageID, "not-ours"}, "delivered", epoch.Add(time.Second)))
	must(t, r.h.RecordReceipt(groupA, chatA, []string{d.MessageID}, "read", epoch.Add(2*time.Second)))
	must(t, r.h.RecordReceipt(groupA, chatA, []string{d.MessageID}, "delivered", epoch.Add(3*time.Second)))
	must(t, r.h.RecordReceipt(groupA, chatB, []string{d.MessageID}, "delivered", epoch.Add(4*time.Second)))
	for _, selector := range []map[string]any{{"key": "receipt"}, {"message_id": d.MessageID}, {"approval_id": d.ID}} {
		out, e = r.call(t, "get_send_status", selector)
		v := n1Draft(t, out, e)
		if v.Status != "read" || v.SentAt == nil || v.DeliveredAt == nil || v.ReadAt == nil || v.DeliveredCount != 2 || v.ReadCount != 1 {
			t.Fatalf("receipt state: %+v", v)
		}
	}
	out, e = r.call(t, "check_send", map[string]any{"approval_id": d.ID})
	if n1Draft(t, out, e).Status != "read" {
		t.Fatal("legacy checker")
	}
	if _, e = r.call(t, "get_send_status", map[string]any{"key": "receipt", "message_id": d.MessageID}); e == nil {
		t.Fatal("ambiguous selector")
	}
	if len(r.fake.Sent) != 1 {
		t.Fatal("receipt caused outbound action")
	}
}

type n1FailedMessenger struct{ calls int }

func (f *n1FailedMessenger) Send(context.Context, model.Approval) (string, error) {
	f.calls++
	return "", errors.New("fake failure")
}
func TestN1FailedSendIsDuplicateSafe(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "free", true)
	fake := &n1FailedMessenger{}
	r.h.Messenger = fake
	for i := 0; i < 2; i++ {
		out, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "x", "key": "failed"})
		if n1Draft(t, out, e).Status != "failed" {
			t.Fatal("failure not retained")
		}
	}
	if fake.calls != 1 {
		t.Fatal("failed send retried")
	}
}
func TestN1ConnectionHealth(t *testing.T) {
	r := newRig(t)
	r.mc.Link = r.link
	check := func(connected bool) {
		t.Helper()
		for _, tool := range []string{"whoami", "check_connection"} {
			out, e := r.call(t, tool, map[string]any{})
			must(t, e)
			v := out.(map[string]any)
			if v["can_send"] != connected || v["whatsapp"].(map[string]any)["connected"] != connected {
				t.Fatal("wrong connection state")
			}
		}
	}
	check(false)
	r.link.Emit(wa.LinkEvent{State: "ready"})
	r.link.Touch(epoch.Add(-2 * time.Minute))
	check(true)
	out, e := r.call(t, "check_connection", map[string]any{})
	must(t, e)
	if out.(map[string]any)["health"] != "Connected · last message 2 min ago" {
		t.Fatal("plain health missing")
	}
	p := r.a.Permissions
	p.RequestSend = false
	r.agent(t, p)
	out, e = r.call(t, "check_connection", map[string]any{})
	must(t, e)
	if out.(map[string]any)["can_send"] != false {
		t.Fatal("permission ignored")
	}
}
func TestN1SetupReadyWithUnsortedChats(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 7; i++ {
		must(t, r.h.Repo.Put("circles", string(rune('a'+i)), model.Circle{ID: string(rune('a' + i)), Name: "Project", Kind: "project", Confirmed: true, SendRule: "confirm"}))
	}
	out, e := r.mc.CircleService().Setup(nil, true)
	must(t, e)
	if out.Stage != "ready" || out.UnsortedActiveChats == 0 || out.People.WithoutDetails == 0 {
		t.Fatal("sorting/details still block setup")
	}
}
func TestN1OutboxRetention(t *testing.T) {
	r := newRig(t)
	out, e := r.call(t, "upload_attachment", map[string]any{"content_text": "old", "filename": "old.txt"})
	must(t, e)
	id := out.(map[string]any)["attachment_id"].(string)
	path := filepath.Join(r.home, "outbox", id)
	old := epoch.Add(-8 * 24 * time.Hour)
	must(t, os.Chtimes(path, old, old))
	r.h.PruneOutbox()
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("old file retained")
	}
	var at model.Attachment
	if r.h.Repo.Get("attachments", id, &at) == nil {
		t.Fatal("old metadata retained")
	}
}
func TestN1InstructionLimitsAndFileMeta(t *testing.T) {
	if utf8.RuneCountInString(mcpserver.Instructions) > 1200 {
		t.Fatal("server instructions exceed 1200 characters")
	}
	b, e := os.ReadFile("../../../integrations/chatgpt/instructions.md")
	must(t, e)
	if utf8.RuneCount(b) > 1500 {
		t.Fatal("ChatGPT instructions exceed 1500 characters")
	}
	for _, path := range []string{"../../../integrations/chatgpt/instructions.md", "../../../integrations/codex/skills/limmr/SKILL.md", "../../../integrations/claude-code/skills/limmr/SKILL.md"} {
		b, e := os.ReadFile(path)
		must(t, e)
		if !strings.Contains(string(b), "use notify_me with a stable key") {
			t.Fatal("missing N1 guidance")
		}
	}

	for _, tool := range []string{"send_file", "upload_attachment", "notify_me"} {
		for _, def := range mcpserver.Catalog() {
			if def.Name == tool {
				b, e := json.Marshal(def.Meta)
				must(t, e)
				if !strings.Contains(string(b), "openai/fileParams") || def.Annotations.ReadOnlyHint {
					t.Fatal("file tool metadata wrong")
				}
			}
		}
	}
}

func TestN1LegacyApprovalReceipt(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "confirm", true)
	ap, e := r.h.RequestSend(r.a, "message", chatA, "hello", "as_agent", "", "")
	must(t, e)
	if ap.Text != "hello" {
		t.Fatal("approval voice missing")
	}
	ap, e = r.h.Decide(context.Background(), ap.ID, true, nil, "")
	must(t, e)
	must(t, r.h.RecordReceipt(chatA, chatA, []string{ap.MessageID}, "read", epoch.Add(time.Second)))
	for _, selector := range []map[string]any{{"approval_id": ap.ID}, {"message_id": ap.MessageID}} {
		out, e := r.call(t, "get_send_status", selector)
		d := n1Draft(t, out, e)
		if d.Status != "read" || d.SentAt == nil || d.ReadAt == nil {
			t.Fatalf("legacy receipt missing: status=%s sent=%v read=%v selector=%v", d.Status, d.SentAt, d.ReadAt, selector)
		}
	}
	out, e := r.call(t, "check_send", map[string]any{"approval_id": ap.ID})
	if n1Draft(t, out, e).Status != "read" {
		t.Fatal("legacy checker missing receipt")
	}
}

type n1Downloader string

func (p n1Downloader) Download(context.Context, string, string) (string, error) {
	return string(p), nil
}
func TestN1MediaTransportProjection(t *testing.T) {
	r := newRig(t)
	path := filepath.Join(r.home, "media", "invented.txt")
	r.mc.Downloader = n1Downloader(path)
	r.mc.RemoteMedia = func(a model.Agent, p, chat, message string) (string, error) {
		if p != path || chat != chatA {
			t.Error("media arguments lost")
		}
		return "https://limmr.example/limmr/media/test", nil
	}
	args := map[string]any{"chat_jid": chatA, "message_id": firstMessage(t, r, chatA).ID}
	out, e := r.call(t, "download_media", args)
	must(t, e)
	if out.(map[string]string)["path"] != path {
		t.Fatal("local path missing")
	}
	a, _, e := r.h.CreateAgent("Remote AI", "chatgpt", "remote")
	must(t, e)
	out, e = r.mc.Call(context.Background(), a.ID, "download_media", args)
	must(t, e)
	v := out.(map[string]string)
	if v["download_url"] == "" || v["path"] != "" {
		t.Fatal("remote path exposed")
	}
}
