package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	push "github.com/SherClockHolmes/webpush-go"
	"github.com/notborges/convomeow/internal/notifications"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func validSubscription(t *testing.T) notifications.Subscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := notifications.Subscription{Endpoint: "https://push.example/subscription", Locale: "en"}
	sub.Keys.P256DH = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	sub.Keys.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	return sub
}

func TestEncryptedPushRequestAndRetryAfter(t *testing.T) {
	private, public, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sub := validSubscription(t)
	transport := &Transport{PrivateKey: private, PublicKey: public, Contact: "https://example.com", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "secret preview") || r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid ") || r.Header.Get("TTL") != "30" {
			t.Fatal("push payload was not encrypted or authenticated")
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"12"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	result, err := transport.Send(context.Background(), sub, notifications.Payload{Version: 1, Text: "secret preview"}, 30*time.Second)
	if err != nil || result.Status != 429 || result.RetryAfter != 12*time.Second {
		t.Fatalf("result: %+v %v", result, err)
	}
}

func TestPushEndpointAndKeyValidation(t *testing.T) {
	sub := validSubscription(t)
	if err := Validate(sub); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://push.example/path", "https://localhost/path", "https://127.0.0.1/path", "https://[::1]/path", "https://169.254.169.254/latest/meta-data", "https://10.0.0.1/path", "https://100.64.0.1/path", "https://user@push.example/path", "https://push.example:8443/path"} {
		sub.Endpoint = endpoint
		if err := Validate(sub); err == nil {
			t.Fatalf("accepted endpoint %s", endpoint)
		}
	}
	sub.Endpoint = "https://push.example/path"
	sub.Keys.Auth = "invalid"
	if err := Validate(sub); err == nil {
		t.Fatal("accepted invalid encryption keys")
	}
	client := NewClient()
	if _, err := client.Get("https://127.0.0.1/"); err == nil {
		t.Fatal("private destination reached")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
}
