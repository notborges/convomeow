package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const sessionLifetime = 30 * 24 * time.Hour

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
