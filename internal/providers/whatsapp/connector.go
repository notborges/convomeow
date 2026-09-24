package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/notborges/convomeow/internal/core"
	appsqlite "github.com/notborges/convomeow/internal/store/sqlite"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type Connector struct {
	container *sqlstore.Container
}

func Open(ctx context.Context, path string) (*Connector, error) {
	container, err := sqlstore.New(ctx, "sqlite3", appsqlite.DSN(path), nil)
	if err != nil {
		return nil, err
	}
	return &Connector{container: container}, nil
}

func (c *Connector) Close() error { return c.container.Close() }

func (c *Connector) ResolveTarget(target core.ConversationTarget) (string, error) {
	if target.Type != "phone_number" || strings.Contains(target.Value, "@") {
		return "", fmt.Errorf("%w: target must be a phone number", core.ErrInvalid)
	}
	jid, err := recipientJID(target.Value)
	if err != nil {
		return "", err
	}
	return jid.String(), nil
}

func (c *Connector) Open(ctx context.Context, identity string, emit func(core.Event)) (core.Session, error) {
	jid, err := types.ParseJID(identity)
	if err != nil {
		return nil, fmt.Errorf("parse device identity: %w", err)
	}
	device, err := c.container.GetDevice(ctx, jid)
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, core.ErrNotFound
	}
	return newSession(device, emit), nil
}

func (c *Connector) New(emit func(core.Event)) (core.Session, error) {
	return newSession(c.container.NewDevice(), emit), nil
}

type session struct {
	client        *whatsmeow.Client
	emit          func(core.Event)
	history       chan *events.HistorySync
	historyMu     sync.Mutex
	historyWG     sync.WaitGroup
	historyClosed bool
	retryMu       sync.Mutex
	retryWaiters  map[types.MessageID]chan *events.MediaRetry
}

func newSession(device *store.Device, emit func(core.Event)) core.Session {
	client := whatsmeow.NewClient(device, nil)
	s := &session{client: client, emit: emit, history: make(chan *events.HistorySync, 2), retryWaiters: make(map[types.MessageID]chan *events.MediaRetry)}
	s.historyWG.Add(1)
	go func() {
		defer s.historyWG.Done()
		for event := range s.history {
			s.importHistory(event)
		}
	}()
	client.AddEventHandler(s.handleEvent)
	return s
}

func (s *session) Identity() string {
	if s.client.Store == nil || s.client.Store.ID == nil {
		return ""
	}
	return s.client.Store.ID.String()
}

func (s *session) Connect() error { return s.client.Connect() }

func (s *session) Close() {
	s.client.Disconnect()
	s.closeHistoryQueue()
	s.closeRetryWaiters()
	s.historyWG.Wait()
}

func (s *session) closeHistoryQueue() {
	s.historyMu.Lock()
	if !s.historyClosed {
		s.historyClosed = true
		close(s.history)
	}
	s.historyMu.Unlock()
}

func (s *session) handleEvent(evt any) {
	if history, ok := evt.(*events.HistorySync); ok {
		s.historyMu.Lock()
		if !s.historyClosed {
			s.history <- history
		}
		s.historyMu.Unlock()
		return
	}
	if retry, ok := evt.(*events.MediaRetry); ok {
		s.retryMu.Lock()
		if waiter := s.retryWaiters[retry.MessageID]; waiter != nil {
			select {
			case waiter <- retry:
			default:
			}
		}
		s.retryMu.Unlock()
		return
	}
	if _, ok := evt.(*events.LoggedOut); ok {
		s.closeHistoryQueue()
		s.closeRetryWaiters()
		s.historyWG.Wait()
	}
	translateEvent(s.emit, evt)
}

func (s *session) Login(ctx context.Context, onChallenge func(core.LoginChallenge)) error {
	qr, err := s.client.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := s.client.Connect(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-qr:
			if !ok {
				return errors.New("QR channel closed before pairing")
			}
			switch item.Event {
			case "code":
				onChallenge(core.LoginChallenge{Type: "qr", Value: item.Code, ExpiresAt: time.Now().Add(item.Timeout)})
			case "success":
				return nil
			case "error":
				if item.Error != nil {
					return item.Error
				}
				return errors.New("pairing failed")
			case "timeout":
				return errors.New("pairing timed out")
			default:
				return fmt.Errorf("pairing stopped: %s", item.Event)
			}
		}
	}
}

func (s *session) PrepareText(recipient string) (core.PreparedText, error) {
	jid, err := recipientJID(recipient)
	if err != nil {
		return core.PreparedText{}, err
	}
	prepared := core.PreparedText{ChatID: jid.String(), ProviderMessageID: string(s.client.GenerateMessageID())}
	if s.client.Store != nil && s.client.Store.ID != nil {
		prepared.SenderID = s.client.Store.ID.ToNonAD().String()
	}
	return prepared, nil
}

func (s *session) SendText(ctx context.Context, prepared core.PreparedText, text string) (core.SentText, error) {
	jid, err := recipientJID(prepared.ChatID)
	if err != nil {
		return core.SentText{}, err
	}
	if prepared.ProviderMessageID == "" {
		return core.SentText{}, fmt.Errorf("%w: missing provider message ID", core.ErrInvalid)
	}
	response, err := s.client.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)}, whatsmeow.SendRequestExtra{ID: types.MessageID(prepared.ProviderMessageID)})
	if err != nil {
		return core.SentText{}, err
	}
	chat := response.Chat
	if chat.IsEmpty() {
		chat = jid
	}
	sent := core.SentText{ChatID: chat.String(), ProviderMessageID: string(response.ID), SenderID: prepared.SenderID, Timestamp: response.Timestamp}
	return sent, nil
}

func (s *session) DownloadMedia(ctx context.Context, source core.MediaSource, file *os.File, maxBytes int64) ([]byte, error) {
	return s.downloadMedia(ctx, source, file, maxBytes)
}

func recipientJID(recipient string) (types.JID, error) {
	recipient = strings.TrimSpace(recipient)
	if strings.Contains(recipient, "@") {
		jid, err := types.ParseJID(recipient)
		if err != nil || jid.IsEmpty() {
			return types.JID{}, core.ErrInvalid
		}
		return jid, nil
	}
	number := strings.TrimPrefix(recipient, "+")
	if len(number) < 7 || len(number) > 15 {
		return types.JID{}, fmt.Errorf("%w: recipient must be a phone number or JID", core.ErrInvalid)
	}
	for _, b := range []byte(number) {
		if b < '0' || b > '9' {
			return types.JID{}, fmt.Errorf("%w: phone number must contain only digits", core.ErrInvalid)
		}
	}
	return types.NewJID(number, types.DefaultUserServer), nil
}

func translateEvent(emit func(core.Event), evt any) {
	switch e := evt.(type) {
	case *events.PairSuccess:
		emit(core.Event{Type: core.EventPaired, Identity: e.ID.String()})
	case *events.Connected:
		emit(core.Event{Type: core.EventConnected})
	case *events.Disconnected:
		emit(core.Event{Type: core.EventDisconnected})
	case *events.LoggedOut:
		emit(core.Event{Type: core.EventLoggedOut, Err: fmt.Errorf("logged out: %s", e.Reason.String())})
	case *events.ClientOutdated:
		emit(core.Event{Type: core.EventError, Err: errors.New("WhatsApp client version is outdated")})
	case *events.StreamReplaced:
		emit(core.Event{Type: core.EventError, Err: errors.New("session was opened by another client")})
	case *events.TemporaryBan:
		emit(core.Event{Type: core.EventError, Err: fmt.Errorf("temporary ban: %s", e.String())})
	case *events.Message:
		if message := translateMessage(e); message != nil {
			emit(core.Event{Type: core.EventMessage, Message: message})
		}
	}
}

func translateMessage(event *events.Message) *core.Message {
	if event == nil || event.Message == nil || event.Info.ID == "" || event.Info.Chat.IsEmpty() {
		return nil
	}
	kind, text := messageContent(event.Message)
	if kind == "" {
		return nil
	}
	direction := "inbound"
	if event.Info.IsFromMe {
		direction = "outbound"
	}
	message := &core.Message{ChatID: event.Info.Chat.String(), ProviderMessageID: string(event.Info.ID),
		Direction: direction, Kind: kind, SenderID: event.Info.Sender.String(), Text: text, OccurredAt: event.Info.Timestamp}
	if attachment := mediaAttachment(event.Message, kind, event.IsViewOnce); attachment != nil {
		message.Attachments = []core.Attachment{*attachment}
	}
	return message
}

func messageContent(message *waE2E.Message) (core.MessageKind, string) {
	if text := message.GetConversation(); text != "" {
		return core.MessageKindText, text
	}
	if text := message.GetExtendedTextMessage().GetText(); text != "" {
		return core.MessageKindText, text
	}
	switch {
	case message.GetImageMessage() != nil:
		return core.MessageKindImage, message.GetImageMessage().GetCaption()
	case message.GetVideoMessage() != nil:
		return core.MessageKindVideo, message.GetVideoMessage().GetCaption()
	case message.GetAudioMessage() != nil:
		return core.MessageKindAudio, ""
	case message.GetDocumentMessage() != nil:
		return core.MessageKindDocument, message.GetDocumentMessage().GetCaption()
	case message.GetStickerMessage() != nil:
		return core.MessageKindSticker, ""
	case message.GetLocationMessage() != nil:
		return core.MessageKindLocation, ""
	case message.GetContactMessage() != nil:
		return core.MessageKindContact, ""
	default:
		return "", ""
	}
}
