package native_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/api/web"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/notifications"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

type notificationSender struct{ sent chan notifications.Payload }

func (s *notificationSender) Send(_ context.Context, _ notifications.Subscription, p notifications.Payload, _ time.Duration) (notifications.Result, error) {
	s.sent <- p
	return notifications.Result{Status: 201}, nil
}

func TestBrowserPushAuthenticationDeliveryFocusAndLogout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	repo, err := sqlite.Open(ctx, filepath.Join(dir, "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repo.CreateAccount(ctx, core.Account{ID: "account", Provider: core.ProviderWhatsApp, ConnectionKind: core.ConnectionKindLinkedDevice, Label: "Personal", ProviderIdentity: "fake-device", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetIdentity(ctx, "account", "fake-device"); err != nil {
		t.Fatal(err)
	}
	connector := &fakeConnector{}
	sender := &notificationSender{sent: make(chan notifications.Payload, 8)}
	service := app.New(repo, connector, nil)
	if err := service.ConfigureNotifications(app.PushOptions{Sender: sender, PublicKey: "public-key", Generation: notifications.Generation("test-token")}); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("app"), 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := native.NewWithWeb(service, "test-token", dir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	call := func(method, path string, body any, cookie *http.Cookie, origin string) *http.Response {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	login := call("POST", "/app/session", map[string]string{"token": "test-token"}, nil, server.URL)
	if login.StatusCode != 204 {
		t.Fatal("login failed")
	}
	cookie := login.Cookies()[0]
	login.Body.Close()
	other := call("POST", "/app/session", map[string]string{"token": "test-token"}, nil, server.URL)
	otherCookie := other.Cookies()[0]
	other.Body.Close()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := notifications.Subscription{Endpoint: "https://push.example/subscription", Locale: "pt-BR", Preview: true}
	sub.Keys.P256DH = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	sub.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	bearerOnly, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/notifications/subscriptions", nil)
	if err != nil {
		t.Fatal(err)
	}
	bearerOnly.Header.Set("Authorization", "Bearer test-token")
	bearerOnly.Header.Set("Origin", server.URL)
	denied, err := server.Client().Do(bearerOnly)
	if err != nil {
		t.Fatal(err)
	}
	if denied.StatusCode != 401 {
		t.Fatalf("bearer token registered a browser recipient: %d", denied.StatusCode)
	}
	denied.Body.Close()
	for _, attempt := range []struct {
		cookie *http.Cookie
		origin string
	}{{nil, server.URL}, {cookie, "https://other.example"}} {
		res := call("POST", "/api/v1/notifications/subscriptions", sub, attempt.cookie, attempt.origin)
		if res.StatusCode != 401 {
			t.Fatalf("unauthorized registration: %d", res.StatusCode)
		}
		res.Body.Close()
	}
	registered := decode[struct {
		ID string `json:"id"`
	}](t, call("POST", "/api/v1/notifications/subscriptions", sub, cookie, server.URL), 200)
	res := call("DELETE", "/api/v1/notifications/subscriptions/"+registered.ID, nil, otherCookie, server.URL)
	if res.StatusCode != 404 {
		t.Fatalf("another session deleted subscription: %d", res.StatusCode)
	}
	res.Body.Close()
	message := core.Message{ChatID: "person@s.whatsapp.net", ProviderMessageID: "live", Direction: "inbound", Text: "hello"}
	connector.emitEvent(core.Event{Type: core.EventMessage, Message: &message})
	var first notifications.Payload
	select {
	case first = <-sender.sent:
	case <-ctx.Done():
		t.Fatal("live message did not produce push")
	}
	if first.Text != "hello" || first.Locale != "pt-BR" || first.AccountLabel != "Personal" {
		t.Fatalf("payload: %+v", first)
	}
	connector.emitEvent(core.Event{Type: core.EventMessage, Message: &message})
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	activity, _ := json.Marshal(map[string]any{"type": "browser.activity", "subscription_id": registered.ID, "conversation_id": first.ConversationID, "focused": true})
	stale, _ := json.Marshal(map[string]any{"type": "browser.activity", "subscription_id": "expired-recipient", "conversation_id": first.ConversationID, "focused": true})
	if err := conn.Write(ctx, websocket.MessageText, stale); err != nil {
		t.Fatal(err)
	}
	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("stale recipient interrupted the resource stream: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, activity); err != nil {
		t.Fatal(err)
	}
	// The pong follows processing of the activity frame on the server read loop.
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	message.ProviderMessageID = "focused"
	connector.emitEvent(core.Event{Type: core.EventMessage, Message: &message})
	select {
	case p := <-sender.sent:
		t.Fatalf("duplicate or focused-chat alert: %+v", p)
	case <-time.After(150 * time.Millisecond):
	}
	conn.CloseNow()
	sub.Preview = false
	decode[struct {
		ID string `json:"id"`
	}](t, call("POST", "/api/v1/notifications/subscriptions", sub, cookie, server.URL), 200)
	message.ProviderMessageID = "private"
	connector.emitEvent(core.Event{Type: core.EventMessage, Message: &message})
	select {
	case p := <-sender.sent:
		if p.Text != "" || p.Kind != "" {
			t.Fatal("private notification leaked message content")
		}
	case <-ctx.Done():
		t.Fatal("background alert did not arrive")
	}
	res = call("DELETE", "/app/session", nil, cookie, server.URL)
	if res.StatusCode != 204 {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	res.Body.Close()
	identity, err := web.New(dir, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/api/v1/notifications/config", nil)
	r.AddCookie(cookie)
	owner, ok := identity.BrowserSession(r)
	if !ok {
		t.Fatal("session metadata missing")
	}
	if _, err := repo.GetSubscription(ctx, registered.ID, owner); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("logout retained recipient: %v", err)
	}
	res = call("POST", "/api/v1/notifications/subscriptions", sub, cookie, server.URL)
	if res.StatusCode != 400 {
		t.Fatalf("late registration accepted after logout: %d", res.StatusCode)
	}
	res.Body.Close()
}
