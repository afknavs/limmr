package wa

import (
	"context"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

func TestLane3OutgoingPushNameNeverBecomesRecipientName(t *testing.T) {
	l := offlineLive(t)
	d := &directory{}
	l.Directory = d
	peer, _ := types.ParseJID(phone)
	self := l.Client.Store.ID.ToNonAD()
	msg := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: self, IsFromMe: true}, PushName: "Alex Owner", ID: "outgoing", Timestamp: time.Now()}, Message: &waProto.Message{Conversation: proto.String("Synthetic message")}}
	check(t, l.ingest(context.Background(), msg))
	for _, p := range d.people {
		if p.PushName == "Alex Owner" || p.Name == "Alex Owner" {
			t.Fatal("outgoing sender name assigned to recipient")
		}
	}
	if recipientPushName(true, "Alex Owner") != "" || recipientPushName(false, "Maya") != "Maya" {
		t.Fatal("history push name selection")
	}
}
