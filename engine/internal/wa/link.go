package wa

import (
	"context"
	"encoding/json"
	"limmr/internal/model"
	"sync"
	"time"
)

type Account struct {
	Name        string `json:"name"`
	PhoneMasked string `json:"phone_masked"`
}
type LinkEvent struct {
	State     string   `json:"state"`
	Code      string   `json:"code,omitempty"`
	ExpiresIn int      `json:"expires_in,omitempty"`
	Progress  int      `json:"progress,omitempty"`
	Chats     int      `json:"chats,omitempty"`
	Messages  int      `json:"messages,omitempty"`
	Account   *Account `json:"account,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Fix       string   `json:"fix,omitempty"`
}
type PairingSource interface {
	Start(context.Context, string, string, func(LinkEvent)) error
	Cancel()
	Unlink(context.Context) error
}
type Link struct {
	lastEvent  *time.Time
	Source     PairingSource
	mu         sync.Mutex
	state      LinkEvent
	watchers   map[chan LinkEvent]bool
	cancel     context.CancelFunc
	generation int
	method     string
	phone      string
}

func NewLink(src PairingSource) *Link {
	return &Link{Source: src, state: LinkEvent{State: "unlinked"}, watchers: map[chan LinkEvent]bool{}}
}
func (l *Link) State() LinkEvent { l.mu.Lock(); defer l.mu.Unlock(); return l.state }
func (l *Link) Emit(e LinkEvent) { l.mu.Lock(); defer l.mu.Unlock(); l.emit(e) }
func (l *Link) emit(e LinkEvent) {
	l.state = e
	for ch := range l.watchers {
		select {
		case ch <- e:
		default:
		}
	}
}
func (l *Link) Start(ctx context.Context, method, phone string) error {
	if method != "qr" && method != "phone" {
		return model.ErrInvalid
	}
	if method == "phone" && (len(phone) < 7 || len(phone) > 16) {
		return model.ErrInvalid
	}
	l.mu.Lock()
	if l.cancel != nil {
		// Asking again for the pairing that is already running is harmless: the caller
		// gets the current state from the event stream. A different method must cancel first.
		same := l.method == method && l.phone == phone
		l.mu.Unlock()
		if same {
			return nil
		}
		return model.ErrConflict
	}
	if l.state.State == "ready" || l.state.State == "syncing" {
		l.mu.Unlock()
		return model.ErrConflict
	}
	if l.Source == nil {
		l.mu.Unlock()
		return model.ErrConflict
	}
	work, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	l.method, l.phone = method, phone
	l.generation++
	gen := l.generation
	l.emit(LinkEvent{State: "connecting"})
	l.mu.Unlock()
	go func() {
		err := l.Source.Start(work, method, phone, func(e LinkEvent) {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.generation == gen && work.Err() == nil {
				l.emit(e)
			}
		})
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.generation == gen {
			l.cancel = nil
			if err != nil && work.Err() == nil {
				l.emit(LinkEvent{State: "error", Reason: "Pairing failed.", Fix: "Start linking again, or check Health."})
			}
		}
	}()
	return nil
}
func (l *Link) Cancel() {
	l.mu.Lock()
	l.generation++
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	source := l.Source
	l.emit(LinkEvent{State: "unlinked"})
	l.mu.Unlock()
	if source != nil {
		source.Cancel()
	}
}
func (l *Link) Unlink(ctx context.Context, confirm bool) error {
	if !confirm {
		return model.ErrInvalid
	}
	l.Cancel()
	if l.Source == nil {
		return nil
	}
	return l.Source.Unlink(ctx)
}
func (l *Link) Subscribe() (chan LinkEvent, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan LinkEvent, 32)
	ch <- l.state
	l.watchers[ch] = true
	return ch, func() { l.mu.Lock(); delete(l.watchers, ch); l.mu.Unlock() }
}

// MarshalJSON keeps required progress/count fields even before the first chunk.
func (e LinkEvent) MarshalJSON() ([]byte, error) {
	type alias LinkEvent
	if e.State != "syncing" {
		return json.Marshal(alias(e))
	}
	return json.Marshal(struct {
		State    string `json:"state"`
		Progress int    `json:"progress"`
		Chats    int    `json:"chats"`
		Messages int    `json:"messages"`
	}{e.State, e.Progress, e.Chats, e.Messages})
}

func (l *Link) Touch(at time.Time) { l.mu.Lock(); defer l.mu.Unlock(); l.lastEvent = &at }
func (l *Link) Connection() (bool, bool, *time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	linked := l.state.Account != nil || l.state.State == "ready" || l.state.State == "syncing"
	var at *time.Time
	if l.lastEvent != nil {
		v := *l.lastEvent
		at = &v
	}
	if src, ok := l.Source.(interface{ Linked() bool }); ok {
		linked = src.Linked()
	}
	if src, ok := l.Source.(interface{ LastEventAt() *time.Time }); ok {
		at = src.LastEventAt()
	}
	connected := l.state.State == "ready" || l.state.State == "syncing"
	if src, ok := l.Source.(interface{ Connected() bool }); ok {
		connected = src.Connected()
	}
	return linked, connected, at
}
