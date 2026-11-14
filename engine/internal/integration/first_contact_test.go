package integration_test

import (
	"context"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"strings"
	"testing"
	"time"
)

const newNumber = "15550100901@s.whatsapp.net"

func firstContactRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	must(t, r.p.PutPerson(model.Person{ID: "new-member", Name: "Nora Vale", JIDs: []string{newNumber}, Notes: "PRIVATE_NOTE", Role: "PRIVATE_ROLE", Details: model.PersonDetails{Emails: []model.LabeledValue{{Value: "private@example.invalid"}}}}))
	must(t, r.p.PutMembership(model.Membership{Person: "new-member", Group: groupA, Admin: true}))
	r.a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{groupA}}
	must(t, r.h.UpdateAgent(r.a))
	return r
}
func contactSend(t *testing.T, r *rig, args map[string]any) (model.Draft, error) {
	t.Helper()
	v, err := r.call(t, "send", args)
	if err != nil {
		return model.Draft{}, err
	}
	d, ok := v.(model.Draft)
	if !ok {
		t.Fatalf("unexpected send response %T", v)
	}
	return d, nil
}
func TestFirstContactLookupProjectsReadableMemberOnly(t *testing.T) {
	r := firstContactRig(t)
	for _, q := range []struct {
		name string
		args map[string]any
	}{
		{"get", map[string]any{"kind": "group", "id": groupA}},
		{"find", map[string]any{"types": []string{"person"}, "query": "Nora"}},
	} {
		out, err := r.call(t, q.name, q.args)
		must(t, err)
		wire := encode(out)
		for _, want := range []string{"new-member", "Nora Vale", "admin", "+15550100901"} {
			if !strings.Contains(wire, want) {
				t.Fatalf("%s missing %s", q.name, want)
			}
		}
		for _, secret := range []string{"PRIVATE_NOTE", "PRIVATE_ROLE", "private@example.invalid"} {
			if strings.Contains(wire, secret) {
				t.Fatalf("%s leaked private profile", q.name)
			}
		}
	}
	// Neither hidden people nor hidden groups are discoverable.
	for _, hidden := range []string{newNumber, groupA} {
		settings := r.h.Settings()
		settings.HiddenChats = []string{hidden}
		must(t, r.h.SetSettings(settings))
		out, err := r.call(t, "find", map[string]any{"types": []string{"person"}, "query": "Nora"})
		must(t, err)
		if strings.Contains(encode(out), "new-member") {
			t.Fatal("hidden member discoverable")
		}
	}
}
func TestFirstContactInstructionSendReceiptAndAccess(t *testing.T) {
	r := firstContactRig(t)
	// Even a free source circle does not make a first contact free.
	must(t, r.h.Repo.Put("circles", "free", model.Circle{ID: "free", Confirmed: true, SendRule: "free", Chats: []model.CircleChat{{ChatJID: groupA}}}))
	d, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Hello, about the sketches.", "owner_instruction": "Please message Nora about the sketches.", "key": "first-nora"})
	must(t, err)
	if d.State != "sent" || !d.FirstContact || d.Rule == "free" || len(r.fake.Sent) != 1 || r.fake.Sent[0].ChatJID != newNumber || !r.fake.Sent[0].FirstContact {
		t.Fatal("first contact did not send exactly once to phone address")
	}
	if d.FirstContactGroup == nil || d.FirstContactGroup.ID != groupA || d.FirstContactPerson.ID != "new-member" || !strings.Contains(d.NextStep, "Reply access") {
		t.Fatal("missing provenance or access explanation")
	}
	if r.h.CanChat(&r.a, newNumber) {
		t.Fatal("reply access widened")
	}
	chats, err := r.m.Chats()
	must(t, err)
	found := false
	for _, c := range chats {
		found = found || c.JID == newNumber
	}
	if !found {
		t.Fatal("sent chat not created")
	}
	receiptAt := epoch.Add(time.Minute)
	must(t, r.h.RecordReceipt(newNumber, newNumber, []string{d.MessageID}, "delivered", receiptAt))
	out, err := r.call(t, "send_status", map[string]any{"message_id": d.MessageID})
	must(t, err)
	status := out.(model.Draft)
	if status.Status != "delivered" || !status.FirstContact || status.FirstContactGroup.Name != "Design Club" {
		t.Fatal("receipt lost first-contact status")
	}
	repeated, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Hello, about the sketches.", "owner_instruction": "Please message Nora.", "key": "first-nora"})
	must(t, err)
	if repeated.ID != d.ID || len(r.fake.Sent) != 1 {
		t.Fatal("idempotency did not preserve first contact")
	}
	acts, err := r.h.Activity(nil, hub.ActivityFilter{})
	must(t, err)
	sent := false
	for _, act := range acts {
		if act.Kind == "sent" {
			sent = act.FirstContact && act.FirstContactGroup.ID == groupA
		}
	}
	if !sent {
		t.Fatal("sent activity missing provenance")
	}
	// A new ordinary send to the now-existing but unreadable DM must be hidden.
	_, err = contactSend(t, r, map[string]any{"recipient": "new-member", "text": "A second message.", "owner_instruction": "Message Nora again."})
	if err != model.ErrNotFound || len(r.fake.Sent) != 1 {
		t.Fatal("new-chat exception became ongoing access")
	}
}
func TestFirstContactAlwaysRequiresOwnerApprovalWithoutNamedInstruction(t *testing.T) {
	for _, preset := range []string{"own", "given", "remote", "remote-transport"} {
		for _, decision := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%t", preset, decision), func(t *testing.T) {
				r := firstContactRig(t)
				if preset == "remote-transport" {
					previous := r.a
					var err error
					r.a, _, err = r.h.CreateAgent("Remote own AI", "chatgpt", "remote")
					must(t, err)
					r.a.Permissions = previous.Permissions
				} else {
					r.a.Preset = preset
				}
				must(t, r.h.UpdateAgent(r.a))
				quote := "Send the club update."
				if preset != "own" {
					quote = "Please message Nora about sketches."
				}
				d, err := contactSend(t, r, map[string]any{"recipient": "+1 (555) 010-0901", "text": "Hello about the club.", "owner_instruction": quote})
				must(t, err)
				if !d.FirstContact || !d.NeedsConfirmation || d.State != "pending" || d.ApprovalID == "" || len(r.fake.Sent) != 0 {
					t.Fatal("new chat bypassed approval")
				}
				// Neither an elapsed chat confirmation nor a forged later instruction can consume a pending approval.
				_, err = r.h.SendDraft(context.Background(), r.a, d.ID, hub.SendOptions{OwnerInstruction: "Message Nora."})
				if err == nil || len(r.fake.Sent) != 0 {
					t.Fatal("agent consumed owner approval")
				}
				ap, err := r.h.Decide(context.Background(), d.ID, decision, nil, "")
				must(t, err)
				if !ap.FirstContact || ap.FirstContactPerson.Name != "Nora Vale" || ap.FirstContactGroup.Name != "Design Club" {
					t.Fatal("approval lost provenance")
				}
				expected := 0
				if decision {
					expected = 1
				}
				if len(r.fake.Sent) != expected {
					t.Fatal("wrong approval delivery count")
				}
				out, err := r.call(t, "send_status", map[string]any{"approval_id": d.ID})
				must(t, err)
				got := out.(model.Draft)
				if decision && got.State != "sent" || !decision && got.State != "denied" {
					t.Fatal("approval status not mirrored")
				}
				if decision {
					must(t, r.h.RecordReceipt(newNumber, newNumber, []string{got.MessageID}, "read", epoch.Add(time.Minute)))
					out, err = r.call(t, "send_status", map[string]any{"approval_id": d.ID})
					must(t, err)
					if out.(model.Draft).Status != "read" {
						t.Fatal("approved send receipt missing")
					}
				}
			})
		}
	}
}
func TestFirstContactPrivacyDenialsMatchUnknown(t *testing.T) {
	for _, mode := range []string{"unreadable-group", "hidden-person", "hidden-group", "unknown", "unknown-person", "reaction", "reply", "no-read", "no-people", "lid-hidden-alias"} {
		t.Run(mode, func(t *testing.T) {
			r := firstContactRig(t)
			args := map[string]any{"recipient": "new-member", "text": "Hello."}
			switch mode {
			case "unreadable-group":
				r.a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{groupB}}
				must(t, r.h.UpdateAgent(r.a))
			case "hidden-person", "hidden-group":
				settings := r.h.Settings()
				settings.HiddenChats = []string{newNumber}
				if mode == "hidden-group" {
					settings.HiddenChats = []string{groupA}
				}
				must(t, r.h.SetSettings(settings))
			case "unknown":
				args["recipient"] = "+1 555 010 0999"
			case "unknown-person":
				args["recipient"] = "missing-person"
			case "reaction":
				delete(args, "text")
				args["react"] = map[string]any{"emoji": "👍", "message_id": "invented"}
			case "reply":
				args["reply_to"] = "invented"
			case "no-read":
				r.a.Permissions.ReadMessages = false
				must(t, r.h.UpdateAgent(r.a))
			case "no-people":
				r.a.Permissions.ReadPeople = false
				must(t, r.h.UpdateAgent(r.a))
			case "lid-hidden-alias":
				must(t, r.p.PutPerson(model.Person{ID: "new-member", Name: "Nora Vale", JIDs: []string{newNumber, "9000000901@lid"}}))
				settings := r.h.Settings()
				settings.HiddenChats = []string{"9000000901@lid"}
				must(t, r.h.SetSettings(settings))
				args["recipient"] = "+1 555 010 0901"
				args["owner_instruction"] = "Message +1 555 010 0901."
			}
			_, err := contactSend(t, r, args)
			if err != model.ErrNotFound || err.Error() != "not found" || len(r.fake.Sent) != 0 || len(r.fake.RecipientChecks) != 0 {
				t.Fatalf("privacy boundary: %v, sends=%d checks=%d", err, len(r.fake.Sent), len(r.fake.RecipientChecks))
			}
			aps, err := r.h.Approvals()
			must(t, err)
			if len(aps) != 0 {
				t.Fatal("denial queued an approval")
			}
		})
	}
}
func TestFirstContactBareNumberRequiresOwnExactQuotedDigits(t *testing.T) {
	for _, mode := range []string{"allowed", "no-quote", "unrelated", "suffix", "given", "spliced"} {
		t.Run(mode, func(t *testing.T) {
			r := firstContactRig(t)
			args := map[string]any{"recipient": "+1 555 010 0999", "text": "Hello.", "owner_instruction": "Please message +1 (555) 010-0999 about the poster."}
			switch mode {
			case "no-quote":
				delete(args, "owner_instruction")
			case "unrelated":
				args["owner_instruction"] = "Message Nora."
			case "suffix":
				args["owner_instruction"] = "Message 5550100999."
			case "given":
				r.a.Preset = "given"
				must(t, r.h.UpdateAgent(r.a))
			case "spliced":
				args["owner_instruction"] = "Reference 1555 then 0100999."
			}
			d, err := contactSend(t, r, args)
			if mode == "allowed" {
				must(t, err)
				if d.State != "sent" || len(r.fake.Sent) != 1 || d.FirstContactGroup != nil {
					t.Fatal("quoted bare number not sent")
				}
			} else if err != model.ErrNotFound || len(r.fake.Sent) != 0 || len(r.fake.RecipientChecks) != 0 {
				t.Fatal("unquoted bare number was probed")
			}
		})
	}
}
func TestFirstContactResolvesLIDAndChecksRegistration(t *testing.T) {
	for _, mode := range []string{"mapped", "unavailable", "not-on-whatsapp"} {
		t.Run(mode, func(t *testing.T) {
			r := firstContactRig(t)
			must(t, r.h.Repo.Put("people", "new-member", model.Person{ID: "new-member", Name: "Nora Vale", JIDs: []string{"9000000901@lid"}}))
			if mode != "unavailable" {
				r.fake.RecipientMappings = map[string]string{"9000000901@lid": newNumber}
			}
			if mode == "not-on-whatsapp" {
				r.fake.NotOnWhatsApp = map[string]bool{newNumber: true}
			}
			d, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Hello.", "owner_instruction": "Message Nora about sketches."})
			if mode == "mapped" {
				must(t, err)
				if d.State != "sent" || r.fake.Sent[0].ChatJID != newNumber {
					t.Fatal("LID not resolved to phone")
				}
			} else {
				if err == nil || len(r.fake.Sent) != 0 {
					t.Fatal("unresolvable contact sent")
				}
				want := "phone number is unavailable"
				if mode == "not-on-whatsapp" {
					want = "not on WhatsApp"
				}
				if !strings.Contains(err.Error(), want) {
					t.Fatal("not actionable", err)
				}
				aps, err := r.h.Approvals()
				must(t, err)
				if len(aps) > 0 {
					t.Fatal("failed preflight queued")
				}
			}
		})
	}
}
func TestFirstContactBackgroundAndDraftOnlyCannotBypassApproval(t *testing.T) {
	for _, mode := range []string{"background", "nightly", "handoff", "draft-only", "queue-requested"} {
		t.Run(mode, func(t *testing.T) {
			r := firstContactRig(t)
			ctx := context.Background()
			o := hub.SendOptions{OwnerInstruction: "Message Nora about sketches."}
			switch mode {
			case "background":
				ctx = hub.WithBackground(ctx, true)
			case "nightly":
				r.a.Name = "limmr-nightly"
				must(t, r.h.UpdateAgent(r.a))
			case "handoff":
				ctx = hub.WithHandoff(ctx, hub.RunIdentity{AgentID: r.a.ID})
				r.a.RunChat = groupA
			case "draft-only":
				o.DraftOnly = true
			case "queue-requested":
				o.QueueForApproval = true
			}
			d, err := r.h.DraftSendKey(ctx, r.a, "message", "new-member", "Hello.", "", "", "", "", o)
			if mode == "handoff" {
				if err != model.ErrNotFound {
					t.Fatal("handoff escaped scope", err)
				}
				return
			}
			must(t, err)
			if len(r.fake.Sent) != 0 {
				t.Fatal("unattended or draft-only contact sent")
			}
			if mode == "draft-only" {
				if d.State != "draft" {
					t.Fatal("draft-only queued")
				}
				d, err = r.h.SendDraft(hub.WithBackground(ctx, true), r.a, d.ID)
				must(t, err)
			}
			if d.State != "pending" {
				t.Fatal("missing approval")
			}
		})
	}
}
func TestFirstContactLimitIsRollingAndSharedAcrossAIs(t *testing.T) {
	r := firstContactRig(t)
	a2, _, err := r.h.CreateAgent("Second AI", "custom", "local")
	must(t, err)
	now := epoch
	r.h.Now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		agent := r.a
		if i%2 == 1 {
			agent = a2
		}
		phone := fmt.Sprintf("+1 555 010 %04d", 920+i)
		_, err := r.h.DraftSendKey(context.Background(), agent, "message", phone, "Hello.", "", "", "", "", hub.SendOptions{OwnerInstruction: "Message " + phone + " about sketches."})
		if i < 5 {
			must(t, err)
		} else if err == nil || !strings.Contains(err.Error(), "limit reached") {
			t.Fatal("sixth contact not refused", err)
		}
	}
	if len(r.fake.Sent) != 5 {
		t.Fatal("wrong limit count")
	}
	aps, err := r.h.Approvals()
	must(t, err)
	if len(aps) != 0 {
		t.Fatal("limit queued something")
	}
	now = epoch.Add(24*time.Hour - time.Nanosecond)
	_, err = r.h.DraftSendKey(context.Background(), r.a, "message", "+1 555 010 0998", "Hello.", "", "", "", "", hub.SendOptions{OwnerInstruction: "Message +1 555 010 0998."})
	if err == nil {
		t.Fatal("rolling window expired too early")
	}
	now = epoch.Add(24 * time.Hour)
	_, err = r.h.DraftSendKey(context.Background(), r.a, "message", "+1 555 010 0998", "Hello.", "", "", "", "", hub.SendOptions{OwnerInstruction: "Message +1 555 010 0998."})
	must(t, err)
	if len(r.fake.Sent) != 6 {
		t.Fatal("window did not expire")
	}
}
func TestFirstContactApprovalRechecksPrivacyAndNeverRules(t *testing.T) {
	for _, mode := range []string{"hide-person", "hide-group", "revoke", "membership", "never-source", "never-target", "not-on-whatsapp", "limit"} {
		t.Run(mode, func(t *testing.T) {
			r := firstContactRig(t)
			d, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Hello."})
			must(t, err)
			switch mode {
			case "hide-person", "hide-group":
				s := r.h.Settings()
				s.HiddenChats = []string{newNumber}
				if mode == "hide-group" {
					s.HiddenChats = []string{groupA}
				}
				must(t, r.h.SetSettings(s))
			case "revoke":
				r.a.Permissions.RequestSend = false
				must(t, r.h.UpdateAgent(r.a))
			case "membership":
				must(t, r.h.Repo.Delete("memberships", groupA+":new-member"))
			case "never-source", "never-target":
				jid := groupA
				if mode == "never-target" {
					jid = newNumber
				}
				must(t, r.h.Repo.Put("circles", "never", model.Circle{ID: "never", Confirmed: true, SendRule: "never", Chats: []model.CircleChat{{ChatJID: jid}}}))
			case "not-on-whatsapp":
				r.fake.NotOnWhatsApp = map[string]bool{newNumber: true}
			case "limit":
				s := r.h.Settings()
				zero := 0
				s.FirstContactLimit = &zero
				must(t, r.h.SetSettings(s))
			}
			_, err = r.h.Decide(context.Background(), d.ID, true, nil, "")
			if err == nil || len(r.fake.Sent) != 0 {
				t.Fatal("approval skipped current rules")
			}
		})
	}
}
func TestFirstContactFileUsesApprovalAndPreservesAttachment(t *testing.T) {
	r := firstContactRig(t)
	out, err := r.call(t, "upload_attachment", map[string]any{"filename": "sketches.txt", "content_text": "Invented sketches"})
	must(t, err)
	id := out.(map[string]any)["attachment_id"].(string)
	d, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Club notes.", "attachment_id": id, "as_document": true})
	must(t, err)
	if d.State != "pending" || d.Attachment == nil || d.Attachment.ID != id {
		t.Fatal("file did not park")
	}
	ap, err := r.h.Decide(context.Background(), d.ID, true, nil, "")
	must(t, err)
	if ap.State != "sent" || len(r.fake.Sent) != 1 || r.fake.Sent[0].Attachment.ID != id || !r.fake.Sent[0].AsDocument {
		t.Fatal("file approval changed attachment")
	}
}
func TestFirstContactSettingOwnerOnly(t *testing.T) {
	r := firstContactRig(t)
	if *r.h.Settings().FirstContactLimit != 5 {
		t.Fatal("default limit")
	}
	s := r.h.Settings()
	limit := 3
	s.FirstContactLimit = &limit
	status(t, r.request(t, "PUT", "/v1/settings", r.token, s, nil), 403)
	status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, s, nil), 200)
	if *r.h.Settings().FirstContactLimit != 3 {
		t.Fatal("owner setting not persisted")
	}
	limit = -1
	if err := r.h.SetSettings(s); err != model.ErrInvalid {
		t.Fatal("invalid limit accepted")
	}
}

// Ensure the modern and compatibility entry points share this behavior.
func TestFirstContactCompatibilitySend(t *testing.T) {
	r := firstContactRig(t)
	out, err := r.mc.Call(context.Background(), r.a.ID, "send_message", mcpserver.Args{"recipient": "new-member", "text": "Hello.", "owner_instruction": "Message Nora."})
	must(t, err)
	if out.(model.Draft).State != "sent" || len(r.fake.Sent) != 1 {
		t.Fatal("legacy send bypassed first contact")
	}
}

// The real HTTP/MCP path uses WithContext and a coordination wrapper. Its
// resolver must survive that wrapper; deferred peer delivery must fail closed.
func TestFirstContactScopedCoordinatorBoundary(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(fmt.Sprint(paired), func(t *testing.T) {
			r := firstContactRig(t)
			coordinator, err := syncer.New(r.h.Repo, r.home, r.h.Now)
			must(t, err)
			r.h.Recipients = r.fake
			coordinator.Bind(r.h)
			if paired {
				must(t, r.h.Repo.Put("settings", "sync_peer", syncer.Peer{EngineID: "invented-peer"}))
			}
			scoped := r.h.WithContext(context.Background())
			d, err := scoped.DraftSendKey(context.Background(), r.a, "message", "new-member", "Hello.", "", "", "", "", hub.SendOptions{OwnerInstruction: "Message Nora."})
			if paired {
				if err == nil || !strings.Contains(err.Error(), "nothing was queued") || len(r.fake.Sent) > 0 {
					t.Fatal("paired first contact was deferred", err)
				}
				var n int
				must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket IN ('send_queue','drafts','approvals')").Scan(&n))
				if n != 0 {
					t.Fatal("paired denial queued work")
				}
			} else {
				must(t, err)
				if d.State != "sent" || len(r.fake.Sent) != 1 {
					t.Fatal("scoped standalone send did not resolve")
				}
			}
		})
	}
}

func TestFirstContactExpiryKeepsActivityProvenance(t *testing.T) {
	r := firstContactRig(t)
	d, err := contactSend(t, r, map[string]any{"recipient": "new-member", "text": "Hello."})
	must(t, err)
	r.h.Now = func() time.Time { return epoch.Add(31 * time.Minute) }
	must(t, r.h.Expire())
	acts, err := r.h.Activity(nil, hub.ActivityFilter{})
	must(t, err)
	found := false
	for _, act := range acts {
		if act.Kind == "expired" {
			found = act.FirstContact && act.FirstContactPerson.ID == "new-member" && act.FirstContactGroup.ID == groupA
		}
	}
	if !found {
		t.Fatal("expired activity lost provenance")
	}
	out, err := r.call(t, "send_status", map[string]any{"approval_id": d.ID})
	must(t, err)
	if out.(model.Draft).Status != "failed" || !out.(model.Draft).FirstContact {
		t.Fatal("expired status lost provenance")
	}
	_, err = r.h.Decide(context.Background(), d.ID, true, nil, "")
	if err == nil || len(r.fake.Sent) != 0 {
		t.Fatal("expired contact sent")
	}
}
