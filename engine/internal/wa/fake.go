package wa

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"sync"
	"time"
)

// Fake never constructs a Whatsmeow client and cannot access the network.
type Fake struct {
	Messages *Store
	mu       sync.Mutex
	Sent     []model.Approval
	Events   []LinkEvent
}

func (f *Fake) Start(ctx context.Context, method, phone string, emit func(LinkEvent)) error {
	events := f.Events
	if len(events) == 0 {
		if method == "phone" {
			events = []LinkEvent{{State: "pair_code", Code: "DEMO1234"}}
		} else {
			events = []LinkEvent{{State: "qr", Code: "limmr-demo-qr-1", ExpiresIn: 60}, {State: "qr", Code: "limmr-demo-qr-2", ExpiresIn: 20}}
		}
		chats, messages := 48, 240
		if f.Messages != nil {
			chats, messages = f.Messages.Counts()
		}
		events = append(events, LinkEvent{State: "syncing", Progress: 100, Chats: chats, Messages: messages}, LinkEvent{State: "ready", Account: &Account{Name: "Demo account", PhoneMasked: "•••• 00"}})
	}
	for _, e := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			emit(e)
		}
	}
	return nil
}
func (f *Fake) Cancel()                      {}
func (f *Fake) Unlink(context.Context) error { return nil }
func (f *Fake) Send(ctx context.Context, a model.Approval) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.Sent = append(f.Sent, a)
	id := a.MessageID
	if id == "" {
		id = "fake-" + dbutil.ID()
	}
	if f.Messages != nil {
		now := time.Now().UTC()
		_ = f.Messages.PutMessage(model.Message{ID: id, ChatJID: a.ChatJID, Sender: "me", Content: a.Text, Timestamp: now, IsFromMe: true})
	}
	return id, nil
}
