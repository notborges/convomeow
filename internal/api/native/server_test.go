package native_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

type fakeConnector struct {
	nextID          atomic.Int64
	sends           atomic.Int64
	failSend        atomic.Bool
	mediaPayload    []byte
	mediaGate       chan struct{}
	mediaDownloads  atomic.Int64
	mediaUploads    atomic.Int64
	mediaSends      atomic.Int64
	failMediaUpload atomic.Bool
	failMediaSend   atomic.Bool
}

func (c *fakeConnector) ResolveTarget(target core.ConversationTarget) (string, error) {
	if target.Type != "phone_number" || target.Value == "" {
		return "", core.ErrInvalid
	}
	return target.Value + "@s.whatsapp.net", nil
}

func (c *fakeConnector) Open(_ context.Context, _ string, emit func(core.Event)) (core.Session, error) {
	return &fakeSession{connector: c, emit: emit}, nil
}

func (c *fakeConnector) New(emit func(core.Event)) (core.Session, error) {
	return &fakeSession{connector: c, emit: emit}, nil
}

func (c *fakeConnector) Close() error { return nil }

type fakeSession struct {
	connector *fakeConnector
	emit      func(core.Event)
}

func (s *fakeSession) Connect() error {
	s.emit(core.Event{Type: core.EventConnected})
	return nil
}

func (s *fakeSession) Login(_ context.Context, _ func(core.LoginChallenge)) error {
	s.emit(core.Event{Type: core.EventPaired, Identity: s.Identity()})
	s.emit(core.Event{Type: core.EventConnected})
	return nil
}

func (s *fakeSession) PrepareMessage(recipient string) (core.PreparedMessage, error) {
	return core.PreparedMessage{ChatID: recipient, ProviderMessageID: fmt.Sprintf("prepared-%d", s.connector.nextID.Add(1))}, nil
}

func (s *fakeSession) SendText(_ context.Context, prepared core.PreparedMessage, _ string) (core.SentMessage, error) {
	s.connector.sends.Add(1)
	if s.connector.failSend.Load() {
		return core.SentMessage{}, errors.New("simulated provider timeout")
	}
	return core.SentMessage{ChatID: prepared.ChatID, ProviderMessageID: prepared.ProviderMessageID, Timestamp: time.Now().UTC()}, nil
}

func (s *fakeSession) UploadMedia(_ context.Context, _ core.MessageKind, data io.Reader, _ *os.File) (core.UploadedMedia, error) {
	s.connector.mediaUploads.Add(1)
	if s.connector.failMediaUpload.Load() {
		return nil, errors.New("simulated media upload failure")
	}
	content, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(content)
	return fakeUploadedMedia{size: int64(len(content)), hash: hash[:]}, nil
}

type fakeUploadedMedia struct {
	size int64
	hash []byte
}

func (u fakeUploadedMedia) Size() int64    { return u.size }
func (u fakeUploadedMedia) SHA256() []byte { return u.hash }

func (s *fakeSession) SendMedia(_ context.Context, prepared core.PreparedMessage, _ core.OutgoingMedia, _ core.UploadedMedia) (core.SentMessage, error) {
	s.connector.mediaSends.Add(1)
	if s.connector.failMediaSend.Load() {
		return core.SentMessage{}, errors.New("simulated media send timeout")
	}
	return core.SentMessage{ChatID: prepared.ChatID, ProviderMessageID: prepared.ProviderMessageID, Timestamp: time.Now().UTC()}, nil
}

func (s *fakeSession) DownloadMedia(ctx context.Context, _ core.MediaSource, file *os.File, maxBytes int64) ([]byte, error) {
	s.connector.mediaDownloads.Add(1)
	if s.connector.mediaGate != nil {
		select {
		case <-s.connector.mediaGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.connector.mediaPayload == nil {
		return nil, core.ErrMediaUnavailable
	}
	if int64(len(s.connector.mediaPayload)) > maxBytes {
		return nil, core.ErrMediaTooLarge
	}
	_, err := file.Write(s.connector.mediaPayload)
	return nil, err
}

func (s *fakeSession) Identity() string { return "test-device" }
func (s *fakeSession) Close()           {}

type testAccount struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type testConversation struct {
	ID string `json:"id"`
}

type testMessage struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type testPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func request(t *testing.T, client *http.Client, method, path string, body any, key string) *http.Response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decode[T any](t *testing.T, response *http.Response, status int) T {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != status {
		var body any
		_ = json.NewDecoder(response.Body).Decode(&body)
		t.Fatalf("HTTP %d, want %d: %v", response.StatusCode, status, body)
	}
	var result T
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNativeAPIConversationSendAndRetry(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	connector := &fakeConnector{}
	service := app.New(store, connector, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	client := server.Client()
	unauthorized, err := client.Get(server.URL + "/api/v1/accounts")
	if err != nil {
		t.Fatal(err)
	}
	problem := decode[struct {
		Code string `json:"code"`
	}](t, unauthorized, http.StatusUnauthorized)
	if problem.Code != "unauthorized" {
		t.Fatalf("unauthorized response: %+v", problem)
	}
	wrongMethod := decode[struct {
		Code string `json:"code"`
	}](t, request(t, client, http.MethodPut, server.URL+"/api/v1/accounts", nil, ""), http.StatusMethodNotAllowed)
	if wrongMethod.Code != "method_not_allowed" {
		t.Fatalf("method response: %+v", wrongMethod)
	}
	plainRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/accounts", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	plainRequest.Header.Set("Authorization", "Bearer test-token")
	plainResponse, err := client.Do(plainRequest)
	if err != nil {
		t.Fatal(err)
	}
	mediaProblem := decode[struct {
		Code string `json:"code"`
	}](t, plainResponse, http.StatusUnsupportedMediaType)
	if mediaProblem.Code != "unsupported_media_type" {
		t.Fatalf("media type response: %+v", mediaProblem)
	}

	versions := decode[struct {
		Versions []string `json:"versions"`
	}](t, request(t, client, http.MethodGet, server.URL+"/api/versions", nil, ""), http.StatusOK)
	if len(versions.Versions) != 1 || versions.Versions[0] != "v1" {
		t.Fatalf("API versions: %+v", versions)
	}
	account := decode[testAccount](t, request(t, client, http.MethodPost, server.URL+"/api/v1/accounts",
		map[string]string{"label": "sales", "provider": "whatsapp", "connection_kind": "linked_device"}, ""), http.StatusCreated)
	if account.ID == "" {
		t.Fatal("account ID is empty")
	}
	loginURL := server.URL + "/api/v1/accounts/" + account.ID + "/login-attempts"
	login := decode[struct {
		ID string `json:"id"`
	}](t, request(t, client, http.MethodPost, loginURL, nil, ""), http.StatusAccepted)
	if login.ID == "" {
		t.Fatal("login attempt ID is empty")
	}
	for attempt := 0; attempt < 50; attempt++ {
		account = decode[testAccount](t, request(t, client, http.MethodGet, server.URL+"/api/v1/accounts/"+account.ID, nil, ""), http.StatusOK)
		if account.State == "connected" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if account.State != "connected" {
		t.Fatalf("account did not connect: %s", account.State)
	}
	conversationURL := server.URL + "/api/v1/accounts/" + account.ID + "/conversations"
	target := map[string]any{"target": map[string]string{"type": "phone_number", "value": "+15551234567"}}
	conversation := decode[testConversation](t, request(t, client, http.MethodPost, conversationURL, target, ""), http.StatusCreated)
	again := decode[testConversation](t, request(t, client, http.MethodPost, conversationURL, target, ""), http.StatusOK)
	if conversation.ID == "" || again.ID != conversation.ID {
		t.Fatalf("conversation changed on repeat: %+v, %+v", conversation, again)
	}
	sendURL := server.URL + "/api/v1/conversations/" + conversation.ID + "/messages"
	firstBody := map[string]any{"kind": "text", "content": map[string]string{"text": "hello"}}
	first := decode[testMessage](t, request(t, client, http.MethodPost, sendURL, firstBody, "request-key-1"), http.StatusCreated)
	repeated := decode[testMessage](t, request(t, client, http.MethodPost, sendURL, firstBody, "request-key-1"), http.StatusCreated)
	if first.ID == "" || first.ID != repeated.ID || first.State != "sent" || connector.sends.Load() != 1 {
		t.Fatalf("send retry duplicated: %+v, %+v, sends=%d", first, repeated, connector.sends.Load())
	}
	conflict := decode[struct {
		Code string `json:"code"`
	}](t, request(t, client, http.MethodPost, sendURL,
		map[string]any{"kind": "text", "content": map[string]string{"text": "different"}}, "request-key-1"), http.StatusConflict)
	if conflict.Code != "idempotency_conflict" {
		t.Fatalf("wrong conflict: %+v", conflict)
	}
	second := decode[testMessage](t, request(t, client, http.MethodPost, sendURL,
		map[string]any{"kind": "text", "content": map[string]string{"text": "second"}}, "request-key-2"), http.StatusCreated)
	firstPage := decode[testPage[testMessage]](t, request(t, client, http.MethodGet, sendURL+"?limit=1", nil, ""), http.StatusOK)
	if len(firstPage.Items) != 1 || firstPage.Items[0].ID != second.ID || firstPage.NextCursor == "" {
		t.Fatalf("first message page: %+v", firstPage)
	}
	secondPage := decode[testPage[testMessage]](t, request(t, client, http.MethodGet,
		sendURL+"?limit=1&cursor="+url.QueryEscape(firstPage.NextCursor), nil, ""), http.StatusOK)
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != first.ID || secondPage.NextCursor != "" {
		t.Fatalf("second message page: %+v", secondPage)
	}
	connector.failSend.Store(true)
	unknown := decode[testMessage](t, request(t, client, http.MethodPost, sendURL,
		map[string]any{"kind": "text", "content": map[string]string{"text": "timeout"}}, "request-key-3"), http.StatusCreated)
	if unknown.State != "outcome_unknown" {
		t.Fatalf("provider timeout state: %+v", unknown)
	}
	unknownRetry := decode[testMessage](t, request(t, client, http.MethodPost, sendURL,
		map[string]any{"kind": "text", "content": map[string]string{"text": "timeout"}}, "request-key-3"), http.StatusCreated)
	if unknownRetry.ID != unknown.ID || connector.sends.Load() != 3 {
		t.Fatalf("unknown send retried: %+v, sends=%d", unknownRetry, connector.sends.Load())
	}
}

func TestHistoryMessagesUseProviderTimeAndHideMediaKeys(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := "history-account"
	now := time.Now().UTC()
	if err := store.CreateAccount(ctx, core.Account{ID: accountID, Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "history", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	chatID := "15551112222@s.whatsapp.net"
	newer, err := store.SaveMessage(ctx, core.Message{AccountID: accountID, ChatID: chatID, ProviderMessageID: "newer",
		Direction: "inbound", Kind: core.MessageKindText, Text: "newer", OccurredAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: accountID, Chat: &core.HistoryChat{ID: chatID}, Messages: []core.Message{{
		ChatID: chatID, ProviderMessageID: "older-image", Direction: "inbound", Kind: core.MessageKindImage, OccurredAt: base,
		Attachments: []core.Attachment{{Kind: core.MessageKindImage, MIMEType: "image/jpeg", Availability: "remote",
			ProviderRef: []byte(`{"media_key":"private-media-secret"}`)}},
	}}}); err != nil {
		t.Fatal(err)
	}
	service := app.New(store, &fakeConnector{}, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	path := server.URL + "/api/v1/conversations/" + newer.ConversationID + "/messages?limit=1"
	first := decode[testPage[struct {
		ID string `json:"id"`
	}]](t, request(t, server.Client(), http.MethodGet, path, nil, ""), http.StatusOK)
	if len(first.Items) != 1 || first.Items[0].ID != newer.ID || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	response := request(t, server.Client(), http.MethodGet, path+"&cursor="+url.QueryEscape(first.NextCursor), nil, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second page HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-media-secret")) || bytes.Contains(data, []byte("provider_ref")) ||
		bytes.Contains(data, []byte("media_key")) || !bytes.Contains(data, []byte(`"availability":"remote"`)) {
		t.Fatalf("media metadata or key exposure: %s", data)
	}
}
