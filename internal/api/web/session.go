package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/notifications"
)

const sessionLifetime = 30 * 24 * time.Hour

func (h *Handler) BrowserSession(r *http.Request) (notifications.Session, bool) {
	if !h.Authenticated(r) {
		return notifications.Session{}, false
	}
	cookie, _ := r.Cookie(sessionCookie)
	parts := strings.Split(cookie.Value, ".")
	expires, _ := strconv.ParseInt(parts[1], 10, 64)
	id := sha256.Sum256([]byte(parts[2]))
	return notifications.Session{ID: hex.EncodeToString(id[:]), Generation: notifications.Generation(h.token), ExpiresAt: time.Unix(expires, 0)}, true
}

func (h *Handler) signSession(payload string) []byte {
	mac := hmac.New(sha256.New, []byte(h.token))
	// Domain separation keeps browser cookies distinct from other token uses.
	mac.Write([]byte("convomeow:browser-session:v1:"))
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (h *Handler) newSession(now time.Time) string {
	payload := "v1." + strconv.FormatInt(now.Add(sessionLifetime).Unix(), 10) + "." + rand.Text()
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.signSession(payload))
}

func (h *Handler) validSession(value string, now time.Time) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != "v1" || parts[2] == "" {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !hmac.Equal(signature, h.signSession(strings.Join(parts[:3], "."))) {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && expires > now.Unix() && expires <= now.Add(sessionLifetime).Unix()
}
