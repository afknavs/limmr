package wa

import (
	"bytes"
	"context"
	"fmt"
	"limmr/internal/model"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

type mediaUploader interface {
	Upload(context.Context, []byte, whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
}

// Use the bytes, rather than a file extension or caller-provided MIME, to choose
// encryption keys and the WhatsApp message shape. Audio has no caption field;
// preserve nonempty text by delivering it as a document.
func outboundMediaType(data []byte, asDocument bool, text string) (string, string, whatsmeow.MediaType) {
	mime := http.DetectContentType(data)
	// Go identifies Ogg as a generic container. Recognize audio codec headers;
	// an unknown/video Ogg container remains a document.
	header := data[:min(len(data), 512)]
	if mime == "application/ogg" && (bytes.Contains(header, []byte("OpusHead")) || bytes.Contains(header, []byte("\x01vorbis"))) {
		mime = "audio/ogg"
	}
	if !asDocument {
		switch {
		case strings.HasPrefix(mime, "image/"):
			return "image", mime, whatsmeow.MediaImage
		case strings.HasPrefix(mime, "video/"):
			return "video", mime, whatsmeow.MediaVideo
		case strings.HasPrefix(mime, "audio/") && text == "":
			return "audio", mime, whatsmeow.MediaAudio
		}
	}
	return "document", mime, whatsmeow.MediaDocument
}

func uploadMediaMessage(ctx context.Context, client mediaUploader, data []byte, a model.Approval, ci *waProto.ContextInfo) (*waProto.Message, error) {
	if a.Attachment == nil {
		return nil, model.ErrInvalid
	}
	kind, mime, mediaType := outboundMediaType(data, a.AsDocument, a.Text)
	up, err := client.Upload(ctx, data, mediaType)
	if err != nil {
		return nil, fmt.Errorf("upload failed")
	}
	switch kind {
	case "image":
		return &waProto.Message{ImageMessage: &waProto.ImageMessage{ContextInfo: ci, Caption: proto.String(a.Text), Mimetype: &mime, URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &up.FileLength}}, nil
	case "video":
		return &waProto.Message{VideoMessage: &waProto.VideoMessage{ContextInfo: ci, Caption: proto.String(a.Text), Mimetype: &mime, URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &up.FileLength}}, nil
	case "audio":
		return &waProto.Message{AudioMessage: &waProto.AudioMessage{ContextInfo: ci, Mimetype: &mime, URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &up.FileLength}}, nil
	default:
		return &waProto.Message{DocumentMessage: &waProto.DocumentMessage{ContextInfo: ci, FileName: proto.String(a.Attachment.Name), Caption: proto.String(a.Text), Mimetype: &mime, URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: &up.FileLength}}, nil
	}
}
