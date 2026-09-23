package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
	"github.com/skip2/go-qrcode"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type message struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	ConversationID string `json:"conversation_id"`
	Direction      string `json:"direction"`
	State          string `json:"state"`
	Kind           string `json:"kind"`
	Content        struct {
		Text    string `json:"text"`
		Caption string `json:"caption"`
	} `json:"content"`
	OccurredAt time.Time `json:"occurred_at"`
}

type conversation struct {
	ID             string   `json:"id"`
	AccountID      string   `json:"account_id"`
	ProviderChatID string   `json:"provider_chat_id"`
	LastMessage    *message `json:"last_message"`
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 40 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, dst any, key ...string) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(key) > 0 {
		req.Header.Set("Idempotency-Key", key[0])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var payload struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		if payload.Detail != "" {
			return fmt.Errorf("%s: %s", payload.Code, payload.Detail)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if dst != nil {
		return json.NewDecoder(resp.Body).Decode(dst)
	}
	return nil
}

func (c *Client) accounts(ctx context.Context) ([]core.AccountStatus, error) {
	var result page[core.AccountStatus]
	if err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) resolve(ctx context.Context, name string) (core.AccountStatus, error) {
	accounts, err := c.accounts(ctx)
	if err != nil {
		return core.AccountStatus{}, err
	}
	for _, account := range accounts {
		if account.ID == name || strings.EqualFold(account.Label, name) {
			return account, nil
		}
	}
	return core.AccountStatus{}, core.ErrNotFound
}

func (c *Client) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	if err := c.requireVersion(ctx); err != nil {
		return err
	}
	switch args[0] {
	case "account":
		return c.account(ctx, args[1:])
	case "chat":
		return c.chat(ctx, args[1:])
	case "message":
		return c.message(ctx, args[1:])
	default:
		return usage()
	}
}

func (c *Client) requireVersion(ctx context.Context) error {
	var result struct {
		Versions []string `json:"versions"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/versions", nil, &result); err != nil {
		return fmt.Errorf("read server API versions: %w", err)
	}
	for _, version := range result.Versions {
		if version == "v1" {
			return nil
		}
	}
	return errors.New("server does not support API v1")
}

func (c *Client) account(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return usage()
		}
		var a core.AccountStatus
		if err := c.do(ctx, http.MethodPost, "/api/v1/accounts", map[string]string{
			"label": args[1], "provider": core.ProviderWhatsApp, "connection_kind": core.ConnectionKindLinkedDevice,
		}, &a); err != nil {
			return err
		}
		fmt.Printf("Created %s (%s)\n", a.Label, a.ID)
		return nil
	case "list":
		if len(args) != 1 {
			return usage()
		}
		accounts, err := c.accounts(ctx)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			fmt.Printf("%-20s %-15s %s\n", a.Label, a.State, a.ID)
		}
		return nil
	case "login":
		if len(args) != 2 {
			return usage()
		}
		return c.login(ctx, args[1])
	default:
		return usage()
	}
}

func (c *Client) login(ctx context.Context, name string) error {
	account, err := c.resolve(ctx, name)
	if err != nil {
		return err
	}
	path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/login-attempts"
	var status core.LoginStatus
	if err := c.do(ctx, http.MethodPost, path, nil, &status); err != nil {
		return err
	}
	path += "/" + url.PathEscape(status.ID)
	fmt.Println("Waiting for WhatsApp pairing QR...")
	lastQR := ""
	lastState := ""
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := c.do(ctx, http.MethodGet, path, nil, &status); err != nil {
			return err
		}
		if status.Challenge != nil && status.Challenge.Type == "qr" && status.Challenge.Value != lastQR {
			lastQR = status.Challenge.Value
			fmt.Println("Scan this QR in WhatsApp → Linked devices:")
			if err := renderQR(os.Stdout, lastQR); err != nil {
				fmt.Println("QR data:", lastQR)
			}
		}
		if status.State != lastState {
			lastState = status.State
			if status.State == "connecting" {
				fmt.Println("Paired; waiting for connection...")
			}
		}
		switch status.State {
		case "connected":
			fmt.Println("Connected.")
			return nil
		case "failed", "expired":
			if status.Error != "" {
				return errors.New(status.Error)
			}
			return fmt.Errorf("login %s", status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) message(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "send":
		sendArgs := args[1:]
		key := uuid.NewString()
		if len(sendArgs) >= 2 && sendArgs[0] == "--key" {
			key = sendArgs[1]
			sendArgs = sendArgs[2:]
		}
		if len(sendArgs) < 3 {
			return usage()
		}
		account, err := c.resolve(ctx, sendArgs[0])
		if err != nil {
			return err
		}
		conversationID := sendArgs[1]
		if _, err := uuid.Parse(conversationID); err == nil {
			var existing conversation
			if err := c.do(ctx, http.MethodGet, "/api/v1/conversations/"+url.PathEscape(conversationID), nil, &existing); err != nil {
				return err
			}
			if existing.AccountID != account.ID {
				return fmt.Errorf("conversation belongs to account %s", existing.AccountID)
			}
		} else {
			path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/conversations"
			var created conversation
			body := map[string]any{"target": map[string]string{"type": "phone_number", "value": conversationID}}
			if err := c.do(ctx, http.MethodPost, path, body, &created); err != nil {
				return err
			}
			conversationID = created.ID
		}
		path := "/api/v1/conversations/" + url.PathEscape(conversationID) + "/messages"
		body := map[string]any{"kind": "text", "content": map[string]string{"text": strings.Join(sendArgs[2:], " ")}}
		var sent message
		if err := c.do(ctx, http.MethodPost, path, body, &sent, key); err != nil {
			return fmt.Errorf("%w (retry with --key %s)", err, key)
		}
		fmt.Printf("Message %s: %s\n", sent.ID, sent.State)
		return nil
	case "list":
		if len(args) != 2 {
			return usage()
		}
		account, err := c.resolve(ctx, args[1])
		if err != nil {
			return err
		}
		path := "/api/v1/messages?account_id=" + url.QueryEscape(account.ID) + "&limit=100"
		var result page[message]
		if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
			return err
		}
		for _, m := range result.Items {
			fmt.Printf("%s %s %-8s %-16s %s\n", m.ID, m.OccurredAt.Local().Format("2006-01-02 15:04"), m.Direction, m.State, messagePreview(m))
		}
		return nil
	default:
		return usage()
	}
}

func (c *Client) chat(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return usage()
		}
		account, err := c.resolve(ctx, args[1])
		if err != nil {
			return err
		}
		path := "/api/v1/conversations?account_id=" + url.QueryEscape(account.ID) + "&limit=100"
		var result page[conversation]
		if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
			return err
		}
		for _, chat := range result.Items {
			preview := ""
			if chat.LastMessage != nil {
				preview = messagePreview(*chat.LastMessage)
			}
			fmt.Printf("%s  %-35s  %s\n", chat.ID, chat.ProviderChatID, preview)
		}
		return nil
	case "messages":
		if len(args) != 3 {
			return usage()
		}
		account, err := c.resolve(ctx, args[1])
		if err != nil {
			return err
		}
		var chat conversation
		if err := c.do(ctx, http.MethodGet, "/api/v1/conversations/"+url.PathEscape(args[2]), nil, &chat); err != nil {
			return err
		}
		if chat.AccountID != account.ID {
			return fmt.Errorf("conversation belongs to account %s", chat.AccountID)
		}
		path := "/api/v1/conversations/" + url.PathEscape(chat.ID) + "/messages?limit=100"
		var result page[message]
		if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
			return err
		}
		for i := len(result.Items) - 1; i >= 0; i-- {
			item := result.Items[i]
			fmt.Printf("%s  %-8s  %s\n", item.OccurredAt.Local().Format("2006-01-02 15:04"), item.Direction, messagePreview(item))
		}
		return nil
	default:
		return usage()
	}
}

func messagePreview(message message) string {
	if message.Kind == "text" || message.Kind == "" {
		return message.Content.Text
	}
	if message.Content.Caption == "" {
		return "[" + message.Kind + "]"
	}
	return "[" + message.Kind + "] " + message.Content.Caption
}

func renderQR(w io.Writer, data string) error {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return err
	}
	bitmap := qr.Bitmap()
	for y := 0; y < len(bitmap); y += 2 {
		_, _ = io.WriteString(w, "\x1b[47m\x1b[30m")
		for x := range bitmap[y] {
			top := bitmap[y][x]
			bottom := y+1 < len(bitmap) && bitmap[y+1][x]
			switch {
			case top && bottom:
				_, _ = io.WriteString(w, "█")
			case top:
				_, _ = io.WriteString(w, "▀")
			case bottom:
				_, _ = io.WriteString(w, "▄")
			default:
				_, _ = io.WriteString(w, " ")
			}
		}
		_, _ = io.WriteString(w, "\x1b[0m\n")
	}
	return nil
}

func usage() error {
	return errors.New("usage: convomeow [--data-dir DIR] serve | account add NAME | account list | account login NAME | chat list ACCOUNT | chat messages ACCOUNT CONVERSATION_ID | message send [--key KEY] ACCOUNT PHONE_OR_CONVERSATION_ID TEXT | message list ACCOUNT")
}
