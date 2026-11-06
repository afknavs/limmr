package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"limmr/internal/hub"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"limmr/internal/model"
	syncer "limmr/internal/sync"
)

func TestN1LocalFileCopyAndRemoteRefusal(t *testing.T) {
	r := newRig(t)
	r.fake.Home = r.home
	// Local paths must sit inside $HOME. Use a throwaway home so the test also passes
	// from a clone or extracted ZIP outside the real home folder (CI, a dot).
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "poster.pdf")
	must(t, os.WriteFile(path, []byte("%PDF-invented original"), 0600))
	out, err := r.call(t, "upload_attachment", map[string]any{"path": path})
	must(t, err)
	id := out.(map[string]any)["attachment_id"].(string)
	must(t, os.WriteFile(path, []byte("changed"), 0600))
	copyBytes, err := os.ReadFile(filepath.Join(r.home, "outbox", id))
	must(t, err)
	if string(copyBytes) != "%PDF-invented original" {
		t.Fatal("outbox changed with original")
	}
	n1Circle(t, r, chatA, "free", true)
	out, err = r.call(t, "send", map[string]any{"recipient": chatA, "attachment_id": id})
	must(t, err)
	d, ok := out.(model.Draft)
	if !ok || d.Attachment == nil || d.Attachment.Name != "poster.pdf" || len(r.fake.Sent) != 1 {
		t.Fatalf("file send: %#v", out)
	}
	if len(r.fake.Uploaded) != 1 || string(r.fake.Uploaded[0]) != "%PDF-invented original" {
		t.Fatal("dispatch did not use approved bytes")
	}
	remote, _, err := r.h.CreateAgent("Remote AI", "custom", "remote")
	must(t, err)
	remote.Permissions.RequestSend = true
	must(t, r.h.UpdateAgent(remote))
	_, err = r.mc.Call(context.Background(), remote.ID, "upload_attachment", map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "only to local agents") {
		t.Fatalf("remote path: %v", err)
	}
}

func TestN1RemoteSixMiBUploadResumeSendAndReceipt(t *testing.T) {
	r := newRig(t)
	r.fake.Home = r.home
	coord, e := syncer.New(r.h.Repo, r.home, r.h.Now)
	must(t, e)
	coord.Bind(r.h)
	r.a.Transport = "remote"
	must(t, r.h.UpdateAgent(r.a))
	n1Circle(t, r, chatA, "free", true)
	data := bytes.Repeat([]byte("invented poster "), (6<<20)/16)
	copy(data, []byte("%PDF-1.7\n"))
	const chunk = 768 << 10
	count := (len(data) + chunk - 1) / chunk
	var upload hub.Upload
	for i := 0; i < count; i++ {
		args := map[string]any{"chunk_index": i, "content_base64": base64.StdEncoding.EncodeToString(data[i*chunk : min((i+1)*chunk, len(data))])}
		if i == 0 {
			args["filename"] = "poster.pdf"
			args["chunk_count"] = count
			args["total_size"] = len(data)
		} else {
			args["upload_id"] = upload.ID
		}
		out, e := r.call(t, "upload_attachment", args)
		must(t, e)
		upload = out.(hub.Upload)
		if i == 0 {
			out, e = r.call(t, "send_status", map[string]any{"upload_id": upload.ID})
			must(t, e)
			resume := out.(hub.Upload)
			if resume.Bytes != chunk || len(resume.Missing) != count-1 {
				t.Fatal("resume progress")
			}
		}
	}
	out, e := r.call(t, "send", map[string]any{"recipient": chatA, "attachment_id": upload.AttachmentID, "text": "Design Club guide", "key": "remote-poster"})
	must(t, e)
	d := out.(model.Draft)
	if len(r.fake.Uploaded) != 1 || !bytes.Equal(r.fake.Uploaded[0], data) {
		t.Fatal("actual dispatch bytes changed")
	}
	if len(r.fake.Sent) != 1 || d.Attachment == nil || d.Attachment.MIME != "application/pdf" || d.Attachment.Size != 6<<20 {
		t.Fatal("file send metadata")
	}
	must(t, r.h.RecordReceipt(chatA, "maya", []string{d.MessageID}, "delivered", r.h.Now().Add(time.Minute)))
	out, e = r.call(t, "send_status", map[string]any{"key": "remote-poster"})
	must(t, e)
	receipt := out.(model.Draft)
	if receipt.Status != "delivered" || receipt.DeliveredCount != 1 || receipt.MessageID != d.MessageID {
		t.Fatalf("receipt: %+v", receipt)
	}
	got, e := os.ReadFile(filepath.Join(r.home, "outbox", upload.AttachmentID))
	must(t, e)
	if !bytes.Equal(got, data) {
		t.Fatal("approved bytes changed")
	}
}

func TestN1WhoamiExplainsSendingSide(t *testing.T) {
	r := newRig(t)
	s, e := syncer.New(r.h.Repo, r.home, r.h.Now)
	must(t, e)
	r.mc.SendingStatus = s.SendingStatus
	must(t, s.Pair("invented-dot", s.EngineID))
	out, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	status := out.(map[string]any)["sending"].(map[string]any)
	if status["holder_side"] != "this_device" || status["last_synced_at"] != nil {
		t.Fatalf("whoami status: %v", status)
	}
	must(t, s.GiveLease())
	out, e = r.call(t, "whoami", map[string]any{})
	must(t, e)
	if out.(map[string]any)["sending"].(map[string]any)["holder_side"] != "peer" {
		t.Fatal("whoami stale holder")
	}
}

func TestN1FileReviewMetadataAndOwnerActions(t *testing.T) {
	for _, caption := range []string{"", "Design Club guide"} {
		t.Run(map[bool]string{true: "captionless", false: "captioned"}[caption == ""], func(t *testing.T) {
			r := newRig(t)
			r.fake.Home = r.home
			n1Circle(t, r, chatA, "free", true)
			out, e := r.call(t, "send", map[string]any{"recipient": chatA, "content_text": "%PDF-invented approval", "filename": "poster.pdf", "text": caption, "queue_for_approval": true})
			must(t, e)
			d := out.(model.Draft)
			if d.State != "suggested" || len(r.fake.Sent) != 0 {
				t.Fatal("file was sent before owner review")
			}
			response := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
			if response.Code != 200 {
				t.Fatal(response.Code)
			}
			var home struct {
				Suggestions []model.Suggestion `json:"suggestions"`
				Queued      []model.Suggestion `json:"queued"`
			}
			must(t, json.Unmarshal(response.Body.Bytes(), &home))
			var found bool
			for _, item := range home.Suggestions {
				if item.ID == d.SuggestionID {
					found = true
					if item.Attachment == nil || item.Attachment.Name != "poster.pdf" || item.Attachment.Size != int64(len("%PDF-invented approval")) || item.ChatJID != chatA || item.DraftText != caption {
						t.Fatalf("file review metadata: %+v", item)
					}
				}
			}
			if !found {
				t.Fatal("file approval missing from shared review source")
			}
			for _, item := range home.Queued {
				if item.ID == d.SuggestionID {
					t.Fatal("file appeared in the text-only approval flow")
				}
			}
			response = r.request(t, "POST", "/v1/suggestions/"+d.SuggestionID+"/send", r.api.ControlToken, map[string]any{}, nil)
			if response.Code != 200 || len(r.fake.Sent) != 1 || len(r.fake.Uploaded) != 1 || string(r.fake.Uploaded[0]) != "%PDF-invented approval" || r.fake.Sent[0].Text != caption {
				t.Fatalf("approved file dispatch: %d", response.Code)
			}
		})
	}
}

func TestN1DeniedFileReviewClosesSendStatus(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "free", true)
	out, e := r.call(t, "send", map[string]any{"recipient": chatA, "content_text": "%PDF-invented denial", "filename": "poster.pdf", "queue_for_approval": true, "key": "denied-poster"})
	must(t, e)
	d := out.(model.Draft)
	response := r.request(t, "POST", "/v1/suggestions/"+d.SuggestionID+"/dismiss", r.api.ControlToken, map[string]any{}, nil)
	if response.Code != 200 || len(r.fake.Sent) != 0 {
		t.Fatal("file denial sent or failed")
	}
	out, e = r.call(t, "send_status", map[string]any{"key": "denied-poster"})
	must(t, e)
	status := out.(model.Draft)
	if status.State != "denied" || status.Status != "failed" {
		t.Fatalf("denied file status: %+v", status)
	}
	if _, e = r.h.SendSuggestion(context.Background(), d.SuggestionID, nil); e == nil {
		t.Fatal("denied file could still send")
	}
}
