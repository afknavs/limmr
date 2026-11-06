package attachments

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// These are deliberately conservative interoperability limits, in MiB. Documents
// are capped at 100 MiB by Limmr even on WhatsApp clients supporting larger files.
const MaxDocument = 100 << 20
const MaxMedia = 16 << 20

func MIME(data []byte, name string) string {
	m := http.DetectContentType(data)
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "M4A ", "M4B ", "M4P ":
			return "audio/mp4"
		}
	}
	if m == "application/ogg" && (bytes.Contains(data[:min(len(data), 512)], []byte("OpusHead")) || bytes.Contains(data[:min(len(data), 512)], []byte("\x01vorbis"))) {
		return "audio/ogg"
	}
	// ZIP-based Office formats need their extension to preserve their document MIME.
	if m == "application/zip" || m == "application/octet-stream" {
		office := map[string]string{
			".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		}
		if t := office[strings.ToLower(filepath.Ext(name))]; t != "" {
			return t
		}
		if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
			return t
		}
	}
	return m
}
func Limit(m string) (string, int64) {
	for _, kind := range []string{"image", "video", "audio"} {
		if strings.HasPrefix(m, kind+"/") {
			return kind + "s", MaxMedia
		}
	}
	return "documents", MaxDocument
}
func CheckSize(size int64, m string) error {
	kind, limit := Limit(m)
	if size > limit {
		return fmt.Errorf("This file is %.1f MiB; Limmr's WhatsApp %s limit is %d MiB. Send a link instead, or compress the file.", float64(size)/(1<<20), kind, limit/(1<<20))
	}
	if size <= 0 {
		return fmt.Errorf("This file is empty. Choose a nonempty file.")
	}
	return nil
}
