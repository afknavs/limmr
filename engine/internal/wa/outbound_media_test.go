package wa

import (
	"bytes"
	"context"
	"errors"
	"limmr/internal/model"
	"testing"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

type fakeMediaUpload struct {
	kind whatsmeow.MediaType
	data []byte
	fail bool
}

func (f *fakeMediaUpload) Upload(_ context.Context, data []byte, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f.kind = kind
	f.data = data
	if f.fail {
		return whatsmeow.UploadResponse{}, errors.New("private upstream error")
	}
	return whatsmeow.UploadResponse{URL: "https://example.invalid/media", DirectPath: "/fake", MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: uint64(len(data))}, nil
}
func TestOutboundMediaWithFakeUploader(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		kind  string
		media whatsmeow.MediaType
	}{
		{"png", []byte("\x89PNG\r\n\x1a\nsynthetic"), "image", whatsmeow.MediaImage},
		{"jpeg", []byte("\xff\xd8\xffsynthetic"), "image", whatsmeow.MediaImage},
		{"gif", []byte("GIF89asynthetic"), "image", whatsmeow.MediaImage},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 synthetic"), "image", whatsmeow.MediaImage},
		{"video", []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), "video", whatsmeow.MediaVideo},
		{"m4a", []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00M4A mp42"), "audio", whatsmeow.MediaAudio},
		{"audio", []byte("OggS\x00OpusHeadsynthetic"), "audio", whatsmeow.MediaAudio},
		{"document", []byte("%PDF-1.7\nsynthetic"), "document", whatsmeow.MediaDocument},
	} {
		for _, forced := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "-media", true: "-document"}[forced], func(t *testing.T) {
				text := "Synthetic caption"
				if tc.kind == "audio" {
					text = ""
				}
				a := model.Approval{Text: text, AsDocument: forced, Attachment: &model.Attachment{Name: "notes.bin"}}
				client := &fakeMediaUpload{}
				ci := &waProto.ContextInfo{StanzaID: proto.String("quoted"), Expiration: proto.Uint32(3600)}
				msg, err := uploadMediaMessage(context.Background(), client, tc.data, a, ci)
				if err != nil {
					t.Fatal(err)
				}
				kind, media := tc.kind, tc.media
				if forced {
					kind, media = "document", whatsmeow.MediaDocument
				}
				actual := ""
				switch {
				case msg.ImageMessage != nil:
					actual = "image"
				case msg.VideoMessage != nil:
					actual = "video"
				case msg.AudioMessage != nil:
					actual = "audio"
				case msg.DocumentMessage != nil:
					actual = "document"
				}
				if actual != kind || client.kind != media || !bytes.Equal(client.data, tc.data) || contextInfo(msg) != ci || extractTextContent(msg) != text {
					t.Fatalf("wrong envelope: %s, %s, expected %s, %s", actual, client.kind, kind, media)
				}
				if msg.DocumentMessage != nil && msg.DocumentMessage.GetFileName() != "notes.bin" {
					t.Fatal("lost filename")
				}
				if msg.ImageMessage != nil && (msg.ImageMessage.GetURL() != "https://example.invalid/media" || msg.ImageMessage.GetFileLength() != uint64(len(tc.data)) || !bytes.Equal(msg.ImageMessage.MediaKey, []byte{1})) {
					t.Fatal("lost upload metadata")
				}
			})
		}
	}
}
func TestAudioTextIsPreservedAndUploadErrorsAreRedacted(t *testing.T) {
	client := &fakeMediaUpload{}
	a := model.Approval{Text: "Audio note", Attachment: &model.Attachment{Name: "note.ogg"}}
	msg, err := uploadMediaMessage(context.Background(), client, []byte("OggS\x00OpusHeadsynthetic"), a, nil)
	if err != nil || msg.GetDocumentMessage().GetCaption() != a.Text {
		t.Fatal("lost audio text")
	}
	client.fail = true
	if _, err = uploadMediaMessage(context.Background(), client, []byte("x"), a, nil); err == nil || err.Error() != "upload failed" {
		t.Fatal("unredacted upload error")
	}
}

func TestDocumentMIMEAndUploadProgress(t *testing.T) {
	client := &fakeMediaUpload{}
	var phases []model.UploadProgress
	ctx := model.WithUploadProgress(context.Background(), func(p model.UploadProgress) error { phases = append(phases, p); return nil })
	data := []byte("PK\x03\x04\x14\x00\x00\x00synthetic office archive")
	a := model.Approval{Text: "Design notes", Attachment: &model.Attachment{Name: "Design notes.docx"}}
	msg, e := uploadMediaMessage(ctx, client, data, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	doc := msg.GetDocumentMessage()
	if doc.GetFileName() != a.Attachment.Name || doc.GetMimetype() != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || doc.GetCaption() != a.Text {
		t.Fatalf("document metadata: %v", doc)
	}
	if len(phases) != 2 || phases[0].Phase != "uploading" || phases[1].Bytes != int64(len(data)) || phases[1].Phase != "uploaded" {
		t.Fatalf("progress: %+v", phases)
	}
}
