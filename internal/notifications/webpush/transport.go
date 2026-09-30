package webpush

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	push "github.com/SherClockHolmes/webpush-go"
	"github.com/notborges/convomeow/internal/notifications"
)

type Transport struct {
	PublicKey  string
	PrivateKey string
	Contact    string
	Client     *http.Client
}

func Validate(s notifications.Subscription) error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || len(s.Endpoint) > 2048 {
		return errors.New("push endpoint must be a public HTTPS URL")
	}
	if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return errors.New("push endpoint must be public")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errors.New("push endpoint must be public")
	}
	key, err := base64.RawURLEncoding.DecodeString(s.Keys.P256DH)
	if err != nil {
		return errors.New("invalid push public key")
	}
	if _, err = ecdh.P256().NewPublicKey(key); err != nil {
		return errors.New("invalid push public key")
	}
	auth, err := base64.RawURLEncoding.DecodeString(s.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("invalid push authentication key")
	}
	if s.Locale != "en" && s.Locale != "pt-BR" {
		return errors.New("unsupported notification locale")
	}
	return nil
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return true
}

func NewClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("push endpoint could not be resolved")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("push endpoint resolves to a non-public address")
			}
		}
		var dial net.Dialer
		for _, ip := range ips {
			conn, err := dial.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("push endpoint connection failed")
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (t *Transport) Send(ctx context.Context, s notifications.Subscription, payload notifications.Payload, ttl time.Duration) (notifications.Result, error) {
	if err := Validate(s); err != nil {
		return notifications.Result{Status: http.StatusBadRequest}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return notifications.Result{}, err
	}
	if len(data) > 3000 {
		return notifications.Result{Status: http.StatusRequestEntityTooLarge}, errors.New("notification payload exceeds limit")
	}
	res, err := push.SendNotificationWithContext(ctx, data, &push.Subscription{Endpoint: s.Endpoint,
		Keys: push.Keys{P256dh: s.Keys.P256DH, Auth: s.Keys.Auth}}, &push.Options{HTTPClient: t.Client,
		Subscriber: t.Contact, VAPIDPublicKey: t.PublicKey, VAPIDPrivateKey: t.PrivateKey, TTL: max(1, int(ttl.Seconds()))})
	if err != nil {
		return notifications.Result{}, errors.New("push delivery failed")
	}
	defer res.Body.Close()
	_, _ = io.CopyN(io.Discard, res.Body, 4096)
	result := notifications.Result{Status: res.StatusCode}
	if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds > 0 {
		result.RetryAfter = time.Duration(min(seconds, 300)) * time.Second
	} else if at, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
		result.RetryAfter = max(0, time.Until(at))
	}
	return result, nil
}
