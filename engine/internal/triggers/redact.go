package triggers

import (
	"bytes"
	"io"
	"sync"
)

type credentialLog struct {
	mu      sync.Mutex
	dst     io.Writer
	secret  []byte
	pending []byte
}

func (w *credentialLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.secret) == 0 {
		return w.dst.Write(p)
	}
	w.pending = append(w.pending, p...)
	out := make([]byte, 0, len(w.pending))
	for len(w.pending) >= len(w.secret) {
		if bytes.HasPrefix(w.pending, w.secret) {
			out = append(out, "[redacted]"...)
			w.pending = w.pending[len(w.secret):]
		} else {
			out = append(out, w.pending[0])
			w.pending = w.pending[1:]
		}
	}
	// Copy the small suffix so a large process write is not retained in memory.
	w.pending = append([]byte(nil), w.pending...)
	_, err := w.dst.Write(out)
	return len(p), err
}
func (w *credentialLog) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.dst.Write(w.pending)
	w.pending = nil
	return err
}
