package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionSurvivesRestartAndRejectsInvalidCookies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("app"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New(dir, "owner-key")
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "http://localhost/app/session", strings.NewReader(`{"token":"owner-key"}`))
	login.Header.Set("Origin", "http://localhost")
	response := httptest.NewRecorder()
	first.ServeHTTP(response, login)
	if response.Code != http.StatusNoContent {
		t.Fatalf("login status: %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != int(sessionLifetime.Seconds()) {
		t.Fatal("missing persistent, protected cookie")
	}
	restarted, err := New(dir, "owner-key")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/accounts", nil)
	request.AddCookie(cookies[0])
	if !restarted.Authenticated(request) {
		t.Fatal("restart invalidated browser session")
	}
	now := time.Now()
	if restarted.validSession(cookies[0].Value, now.Add(sessionLifetime+time.Second)) {
		t.Fatal("expired session accepted")
	}
	rotated := &Handler{token: "new-owner-key"}
	if rotated.validSession(cookies[0].Value, now) {
		t.Fatal("token rotation did not invalidate session")
	}
	parts := strings.Split(cookies[0].Value, ".")
	parts[1] = "9999999999"
	for _, invalid := range []string{"", "owner-key", "v1.bad.nonce.signature", strings.Join(parts, "."), cookies[0].Value + "x"} {
		if restarted.validSession(invalid, now) {
			t.Fatal("invalid session accepted")
		}
	}
	request.Method = http.MethodPost
	request.Header.Set("Origin", "https://other.example")
	if restarted.Authenticated(request) {
		t.Fatal("cross-origin write accepted")
	}
}
