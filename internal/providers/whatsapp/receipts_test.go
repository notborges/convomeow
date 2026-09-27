package whatsapp

import (
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestReceiptTranslationIgnoresOwnAndProtocolAcknowledgments(t *testing.T) {
	event := events.Receipt{MessageSource: types.MessageSource{Chat: types.NewJID("123", types.DefaultUserServer), Sender: types.NewJID("123", types.DefaultUserServer)}, MessageIDs: []types.MessageID{"m"}, Timestamp: time.Now(), Type: types.ReceiptTypeRead}
	if got := translateReceipt(&event); got == nil || got.Kind != "read" || got.ParticipantID != "123@s.whatsapp.net" {
		t.Fatalf("receipt: %+v", got)
	}
	for _, kind := range []types.ReceiptType{types.ReceiptTypeSender, types.ReceiptTypeReadSelf, types.ReceiptTypeRetry, types.ReceiptTypePlayed, types.ReceiptTypePeerMsg} {
		event.Type = kind
		if translateReceipt(&event) != nil {
			t.Fatalf("accepted %s", kind)
		}
	}
	event.Type = types.ReceiptTypeRead
	event.IsFromMe = true
	if translateReceipt(&event) != nil {
		t.Fatal("accepted own read")
	}
	event.IsFromMe = false
	event.MessageSender = types.NewJID("other", types.DefaultUserServer)
	if translateReceipt(&event) != nil {
		t.Fatal("accepted own-device group read")
	}
}

func TestHistoryReceiptsUseReportedTimes(t *testing.T) {
	info := &waWeb.WebMessageInfo{UserReceipt: []*waWeb.UserReceipt{{UserJID: proto.String("123@s.whatsapp.net"), ReceiptTimestamp: proto.Int64(100), ReadTimestamp: proto.Int64(200)}}}
	m := core.Message{ChatID: "group@g.us", ProviderMessageID: "m", Direction: "outbound"}
	receipts := historyReceipts(info, m, true)
	if len(receipts) != 2 {
		t.Fatal(receipts)
	}
	for _, r := range receipts {
		if !r.Group || r.ParticipantID != "123@s.whatsapp.net" || (r.Kind == "read" && r.At.Unix() != 200) || (r.Kind == "delivered" && r.At.Unix() != 100) {
			t.Fatalf("bad history receipt: %+v", r)
		}
	}
	info.UserReceipt = nil
	info.Status = waWeb.WebMessageInfo_READ.Enum()
	if len(historyReceipts(info, m, true)) != 0 {
		t.Fatal("invented participants or times from aggregate status")
	}
}
