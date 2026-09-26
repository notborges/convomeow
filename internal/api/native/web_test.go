package native_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func TestWebSessionUsesCookieAndKeepsBearerAPI(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<h1>Inbox</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("app"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.Open(ctx, filepath.Join(root, "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(repo, &fakeConnector{}, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler, err := native.NewWithWeb(service, "owner-key", webDir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	for _, path := range []string{"/app/", "/app/accounts/one/chats", "/app/assets/app.js"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("web route %s: status=%d body=%q", path, response.StatusCode, body)
		}
	}
	response, err := client.Get(server.URL + "/app/assets/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset: %d", response.StatusCode)
	}
	request := func(method, path, origin, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		result, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		result.Body.Close()
		return result
	}
	if response := request(http.MethodGet, "/api/v1/accounts", "", ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous API: %d", response.StatusCode)
	}
	if response := request(http.MethodPost, "/app/session", "https://other.example", `{"token":"owner-key"}`); response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin login: %d", response.StatusCode)
	}
	if response := request(http.MethodPost, "/app/session", server.URL, `{"token":"owner-key"}`); response.StatusCode != http.StatusNoContent {
		t.Fatalf("owner login: %d", response.StatusCode)
	}
	if response := request(http.MethodGet, "/api/v1/accounts", "", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("browser API: %d", response.StatusCode)
	}
	wsCtx, cancelWS := context.WithTimeout(ctx, 3*time.Second)
	defer cancelWS()
	socketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/events"
	conn, _, err := websocket.Dial(wsCtx, socketURL, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {server.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(wsCtx); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	conn.CloseNow()
	rejected, upgrade, err := websocket.Dial(wsCtx, socketURL, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Origin": {"https://other.example"}}})
	if rejected != nil {
		rejected.CloseNow()
		t.Fatal("cross-origin cookie upgrade accepted")
	}
	if err == nil || upgrade == nil || upgrade.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin cookie upgrade: %v %v", upgrade, err)
	}
	account := `{"label":"one","provider":"whatsapp","connection_kind":"linked_device"}`
	if response := request(http.MethodPost, "/api/v1/accounts", "https://other.example", account); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-origin write: %d", response.StatusCode)
	}
	if response := request(http.MethodPost, "/api/v1/accounts", server.URL, account); response.StatusCode != http.StatusCreated {
		t.Fatalf("browser write: %d", response.StatusCode)
	}
	if response := request(http.MethodDelete, "/app/session", server.URL, ""); response.StatusCode != http.StatusNoContent {
		t.Fatalf("owner logout: %d", response.StatusCode)
	}
	if response := request(http.MethodGet, "/api/v1/accounts", "", ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logged-out API: %d", response.StatusCode)
	}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/accounts", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer owner-key")
	response, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bearer API: %d", response.StatusCode)
	}
}
