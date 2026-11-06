package sync_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"limmr/internal/wa"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func bind(t *testing.T, e *engine) *wa.Fake {
	t.Helper()
	f := e.h.Messenger.(*wa.Fake)
	f.Home = e.home
	e.s.Bind(e.h)
	return f
}
func queuedApproval(e *engine, id string) model.Approval {
	return model.Approval{ID: id, AgentID: "invented-agent", MessageID: "MSG-" + id, ChatJID: "15550000001@s.whatsapp.net", Text: "Design Club session guide", Kind: "message", State: "approved", ExpiresAt: e.now.Add(7 * 24 * time.Hour)}
}
func settle(t *testing.T, a, b *engine) {
	t.Helper()
	for i := 0; i < 4; i++ {
		exchange(t, a, b)
	}
}
func TestQueuedSendHandoverAndDuplicatePrevention(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	q := queuedApproval(a, "queued-handover")
	_, e := a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.GiveLease())
	settle(t, a, b)
	if len(fa.Sent) != 0 || len(fb.Sent) != 1 || fb.Sent[0].MessageID != q.MessageID {
		t.Fatalf("handover: %d %d", len(fa.Sent), len(fb.Sent))
	}
	for _, side := range []*engine{a, b} {
		id, e := side.s.Send(ctx, q)
		must(t, e)
		if id != q.MessageID {
			t.Fatal("identity changed")
		}
	}
	settle(t, a, b)
	if len(fa.Sent)+len(fb.Sent) != 1 {
		t.Fatal("duplicate send")
	}
}
func TestQueuedFileMovesWithSendAndSurvivesRestart(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	data := bytes.Repeat([]byte("invented file"), 500000)
	copy(data, []byte("%PDF-1.7\n"))
	at, e := a.h.Attachment(data, "poster.pdf")
	must(t, e)
	q := queuedApproval(a, "queued-file")
	q.Kind = "file"
	q.Attachment = &at
	_, e = a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, os.Remove(filepath.Join(a.home, "outbox", at.ID)))
	must(t, a.s.GiveLease())
	settle(t, a, b)
	if len(fb.Uploaded) != 1 || !bytes.Equal(fb.Uploaded[0], data) {
		t.Fatal("moved file bytes at dispatch")
	}
	if len(fa.Sent) != 0 || len(fb.Sent) != 1 {
		t.Fatalf("send count %d %d", len(fa.Sent), len(fb.Sent))
	}
	got, e := os.ReadFile(filepath.Join(b.home, "outbox", at.ID))
	must(t, e)
	if !bytes.Equal(data, got) || dbutil.Digest(string(got)) != at.SHA256 {
		t.Fatal("copied file changed")
	}
	restarted, e := syncer.New(b.h.Repo, b.home, func() time.Time { return b.now })
	must(t, e)
	b.s = restarted
	b.h.Messenger = fb
	b.s.Bind(b.h)
	must(t, b.s.Coordinate(ctx))
	if len(fb.Sent) != 1 {
		t.Fatal("restart repeated send")
	}
}
func TestBothOfflineExpiryRecoveryAndOwnerRule(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	_, e := b.s.Send(ctx, queuedApproval(b, "offline"))
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	for _, side := range []*engine{a, b} {
		side.now = side.now.Add(72 * time.Hour)
		must(t, side.clock())
		must(t, side.s.Coordinate(ctx))
		if side.s.HoldsLease() {
			t.Fatal("expired holder active")
		}
	}
	if e = b.s.RecoverLease(false); e == nil {
		t.Fatal("missing owner attestation")
	}
	if len(fa.Sent)+len(fb.Sent) != 0 {
		t.Fatal("partition send")
	}
	settle(t, a, b)
	if len(fa.Sent) != 1 || len(fb.Sent) != 0 {
		t.Fatalf("recovery %d %d", len(fa.Sent), len(fb.Sent))
	}
	// A later partition can be recovered only after expiry and explicit stop.
	a.now = a.now.Add(time.Hour)
	b.now = b.now.Add(time.Hour)
	must(t, a.clock())
	must(t, b.clock())
	must(t, b.s.RecoverLease(true))
	if a.s.MayRunAutomations(ctx) || !b.s.MayRunAutomations(ctx) {
		t.Fatal("recovery fence")
	}
	_, e = b.s.Send(ctx, queuedApproval(b, "owner-recovery"))
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, b.s.Coordinate(ctx))
	if len(fb.Sent) != 0 {
		t.Fatal("recovery sent before ledger reconciliation")
	}
	settle(t, a, b)
	if len(fb.Sent) != 1 {
		t.Fatal("recovered holder did not dispatch after sync")
	}
	if a.s.HoldsLease() || !b.s.HoldsLease() {
		t.Fatal("old epoch resurrected")
	}
}
func TestPeerAcknowledgementAndInFlightFence(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	_, e := a.s.Send(ctx, queuedApproval(a, "fenced"))
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.Coordinate(ctx))
	if len(fa.Sent) != 0 {
		t.Fatal("send without peer ack")
	}
	if e = a.s.GiveLease(); e == nil {
		t.Fatal("handover crossed active claim")
	}
	a.now = a.now.Add(time.Hour)
	b.now = b.now.Add(time.Hour)
	// Exchange just the claim to the peer, but no acknowledgement back.
	page, e := a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
	must(t, e)
	_, e = b.s.Push(ctx, syncer.PushRequest{Changes: page.Changes, FromEngineID: a.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
	must(t, e)
	if e = b.s.RecoverLease(true); e == nil {
		t.Fatal("recovery crossed peer claim")
	}
	if len(fa.Sent)+len(fb.Sent) != 0 {
		t.Fatal("expired claim sent")
	}
}
func TestStandaloneDurableSendIdentity(t *testing.T) {
	a := newEngine(t)
	f := bind(t, a)
	q := queuedApproval(a, "standalone")
	for i := 0; i < 2; i++ {
		id, e := a.s.Send(ctx, q)
		must(t, e)
		if id != q.MessageID {
			t.Fatal("identity")
		}
	}
	if len(f.Sent) != 1 {
		t.Fatal("standalone duplicated")
	}
}

func TestUnpairQuarantinesImportedQueue(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	q := queuedApproval(b, "unpair-origin")
	_, e := b.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	// Sync using services without a dispatcher, leaving the send queued.
	a.s.Messenger = nil
	b.s.Messenger = nil
	exchange(t, a, b)
	must(t, a.s.Unpair())
	must(t, b.s.Unpair())
	if b.s.MayRunAutomations(ctx) {
		t.Fatal("unpair bypassed partition fence")
	}
	must(t, b.s.Unpair())
	if b.s.MayRunAutomations(ctx) {
		t.Fatal("repeated unpair cleared the fence")
	}
	a.now = a.now.Add(time.Hour)
	b.now = b.now.Add(time.Hour)
	if b.s.MayRunAutomations(ctx) {
		t.Fatal("unpair expiry omitted owner confirmation")
	}
	must(t, b.s.RecoverLease(true))
	a.s.Messenger = fa
	b.s.Messenger = fb
	must(t, a.s.Coordinate(ctx))
	must(t, b.s.Coordinate(ctx))
	if len(fa.Sent) != 0 || len(fb.Sent) != 1 {
		t.Fatalf("unpaired duplicate: %d %d", len(fa.Sent), len(fb.Sent))
	}
}

type uncertainMessenger struct{ calls int }

func (f *uncertainMessenger) Send(_ context.Context, a model.Approval) (string, error) {
	f.calls++
	return "", errors.New("lost acknowledgement")
}
func TestAmbiguousSendNeverRetriesOrHandsOff(t *testing.T) {
	a := newEngine(t)
	f := &uncertainMessenger{}
	a.h.Messenger = f
	a.s.Bind(a.h)
	_, e := a.s.Send(ctx, queuedApproval(a, "uncertain"))
	if !errors.Is(e, hub.ErrSendUncertain) {
		t.Fatal(e)
	}
	must(t, a.s.Coordinate(ctx))
	_, _ = a.s.Send(ctx, queuedApproval(a, "uncertain"))
	if f.calls != 1 {
		t.Fatal("ambiguous send retried")
	}
	if a.s.Unpair() == nil {
		t.Fatal("uncertain send allowed unpair")
	}
	q := queuedApproval(a, "uncertain")
	must(t, a.h.Messages.PutChat(model.Chat{JID: q.ChatJID, Name: "Maya Rao"}))
	must(t, a.h.Messages.PutMessage(model.Message{ID: q.MessageID, ChatJID: q.ChatJID, Sender: "me", Content: q.Text, IsFromMe: true, Timestamp: a.now}))
	must(t, a.s.Coordinate(ctx))
	id, e := a.s.Send(ctx, q)
	must(t, e)
	if id != q.MessageID || f.calls != 1 {
		t.Fatal("echo reconciliation repeated dispatch")
	}
	must(t, a.s.Unpair())
}

func TestStaleEpochAndQueuePageCannotResurrectSend(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	q := queuedApproval(a, "stale-page")
	_, e := a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.GiveLease())
	settle(t, a, b)
	staleLease, _ := json.Marshal(syncer.Lease{EngineID: a.s.EngineID, Epoch: 1, RenewedAt: a.now.Add(time.Hour), ExpiresAt: a.now.Add(2 * time.Hour)})
	staleQueue, _ := json.Marshal(syncer.QueuedSend{ID: q.ID, Origin: a.s.EngineID, Approval: q, Phase: "queued", At: a.now})
	_, e = b.s.Push(ctx, syncer.PushRequest{FromEngineID: a.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema, Changes: []syncer.Change{
		{Bucket: "settings", ID: "automation_lease", HLC: 1 << 60, Origin: a.s.EngineID, Op: "upsert", Data: staleLease},
		{Bucket: "send_queue", ID: q.ID, HLC: 1 << 60, Origin: a.s.EngineID, Op: "upsert", Data: staleQueue},
	}})
	must(t, e)
	settle(t, a, b)
	if a.s.HoldsLease() || !b.s.HoldsLease() || len(fa.Sent) != 0 || len(fb.Sent) != 1 {
		t.Fatal("stale page bypassed fencing")
	}
}

func TestOwnerRequestsHandoverFromNonHolder(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	exchange(t, a, b)
	must(t, b.s.RequestLease())
	if b.s.MayRunAutomations(ctx) {
		t.Fatal("request granted premature authority")
	}
	settle(t, a, b)
	if a.s.MayRunAutomations(ctx) || !b.s.MayRunAutomations(ctx) {
		t.Fatal("owner request did not hand over")
	}
}

func TestHigherEpochFencesUnacknowledgedLocalClaim(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	q := queuedApproval(a, "unacknowledged")
	_, e := a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.Coordinate(ctx))
	if len(fa.Sent) != 0 {
		t.Fatal("unacknowledged send")
	}
	a.now = a.now.Add(time.Hour)
	b.now = b.now.Add(time.Hour)
	must(t, b.s.RecoverLease(true))
	settle(t, a, b)
	if a.s.HoldsLease() || !b.s.HoldsLease() || len(fa.Sent) != 0 || len(fb.Sent) != 1 {
		t.Fatal("stale local claim blocked fenced recovery")
	}
}
func TestOutboundOnlyPeerRenewsPassiveHolder(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	for i := 0; i < 12; i++ {
		a.now = a.now.Add(5 * time.Minute)
		b.now = b.now.Add(5 * time.Minute)
		must(t, b.s.Exchange(ctx, syncer.HTTPInProcess{Remote: a.s}))
	}
	if !a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
		t.Fatal("passive Mac holder was not renewed by outbound-only dot")
	}
}

func TestOwnerRecoveryFencesAnUnobservedRenewal(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	start := a.now
	a.now = start.Add(5 * time.Minute)
	b.now = a.now
	// The final completion renews the listening holder after the last pull page.
	must(t, b.s.Exchange(ctx, syncer.HTTPInProcess{Remote: a.s}))
	a.now = start.Add(31 * time.Minute)
	b.now = a.now
	must(t, b.s.RecoverLease(true))
	settle(t, a, b)
	if a.s.HoldsLease() || !b.s.HoldsLease() {
		t.Fatal("unobserved old renewal resurrected the stopped holder")
	}
}

func TestLeaseAuthorityChainSurvivesCompaction(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	a.now = a.now.Add(time.Hour)
	b.now = a.now
	must(t, b.s.RecoverLease(true))
	must(t, b.s.GiveLease())
	for after := int64(0); ; {
		next, _, e := b.h.Repo.CompactSyncBatch(ctx, after, 256)
		must(t, e)
		if next == after {
			break
		}
		after = next
	}
	settle(t, a, b)
	if !a.s.HoldsLease() || b.s.HoldsLease() {
		t.Fatal("offline authority chain lost by compaction")
	}
}

func TestPausedPeerRefusesSyncAndDispatch(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, _ := bind(t, a), bind(t, b)
	pair(t, a, b)
	q := queuedApproval(a, "paused")
	_, e := a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.Coordinate(ctx))
	// The peer may acknowledge the claim, but a local pause still fences dispatch.
	page, e := a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
	must(t, e)
	_, e = b.s.Push(ctx, syncer.PushRequest{Changes: page.Changes, FromEngineID: a.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
	must(t, e)
	var ack syncer.SendAck
	must(t, b.h.Repo.Get("send_acks", q.ID, &ack))
	put(t, a, "send_acks", q.ID, ack)
	var peer syncer.Peer
	must(t, a.h.Repo.Get("settings", "sync_peer", &peer))
	peer.Paused = true
	put(t, a, "settings", "sync_peer", peer)
	must(t, a.s.Coordinate(ctx))
	if len(fa.Sent) != 0 {
		t.Fatal("paused dispatch")
	}
	if _, e = a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema}); e == nil {
		t.Fatal("paused sync accepted")
	}
	peer.Paused = false
	put(t, a, "settings", "sync_peer", peer)
	must(t, a.s.Coordinate(ctx))
	if len(fa.Sent) != 1 {
		t.Fatal("resume did not deliver")
	}
}

func TestConcurrentFileWorkersDispatchOneCompleteCopy(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	data := bytes.Repeat([]byte("invented poster "), (1<<20)/16)
	copy(data, []byte("%PDF-1.7\n"))
	file, e := a.h.Attachment(data, "poster.pdf")
	must(t, e)
	q := queuedApproval(a, "concurrent-file")
	q.Kind = "file"
	q.Attachment = &file
	_, e = a.s.Send(ctx, q)
	if !errors.Is(e, hub.ErrSendQueued) {
		t.Fatal(e)
	}
	must(t, a.s.GiveLease())
	raw := b.s.Messenger
	b.s.Messenger = nil
	exchange(t, a, b)
	b.s.Messenger = raw
	must(t, b.s.Coordinate(ctx))
	b.s.Messenger = nil
	exchange(t, a, b)
	b.s.Messenger = raw
	errorsCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errorsCh <- b.s.Coordinate(ctx) }()
	}
	for i := 0; i < 2; i++ {
		must(t, <-errorsCh)
	}
	if len(fa.Sent) != 0 || len(fb.Sent) != 1 || len(fb.Uploaded) != 1 || !bytes.Equal(fb.Uploaded[0], data) {
		t.Fatal("concurrent workers repeated or truncated file dispatch")
	}
}
