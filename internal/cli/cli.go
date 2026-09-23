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

	"github.com/notborges/convomeow/internal/core"
	"github.com/skip2/go-qrcode"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 40 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, dst any) error {
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
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		if payload.Error.Message != "" {
			return fmt.Errorf("%s: %s", payload.Error.Code, payload.Error.Message)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if dst != nil {
		return json.NewDecoder(resp.Body).Decode(dst)
	}
	return nil
}

func (c *Client) accounts(ctx context.Context) ([]core.AccountStatus, error) {
	var accounts []core.AccountStatus
	if err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, &accounts); err != nil {
		return nil, err
	}
	return accounts, nil
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
		if err := c.do(ctx, http.MethodPost, "/api/v1/accounts", map[string]string{"label": args[1]}, &a); err != nil {
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
	path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/login"
	var status core.LoginStatus
	if err := c.do(ctx, http.MethodPost, path, map[string]any{}, &status); err != nil {
		return err
	}
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
		if len(args) < 4 {
			return usage()
		}
		account, err := c.resolve(ctx, args[1])
		if err != nil {
			return err
		}
		path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/messages"
		var response json.RawMessage
		if err := c.do(ctx, http.MethodPost, path, map[string]string{"to": args[2], "text": strings.Join(args[3:], " ")}, &response); err != nil {
			return err
		}
		var envelope struct {
			Message core.Message `json:"message"`
			Warning string       `json:"warning"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil {
			return err
		}
		message := envelope.Message
		if message.ProviderMessageID == "" {
			if err := json.Unmarshal(response, &message); err != nil {
				return err
			}
		}
		if envelope.Warning != "" {
			fmt.Println(envelope.Warning)
		}
		fmt.Println("Message ID:", message.ProviderMessageID)
		return nil
	case "list":
		if len(args) != 2 {
			return usage()
		}
		account, err := c.resolve(ctx, args[1])
		if err != nil {
			return err
		}
		path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/messages?limit=100"
		var messages []core.Message
		if err := c.do(ctx, http.MethodGet, path, nil, &messages); err != nil {
			return err
		}
		for _, m := range messages {
			fmt.Printf("%d %s %-8s %-25s %s\n", m.ID, m.OccurredAt.Local().Format("2006-01-02 15:04"), m.Direction, m.ChatID, messagePreview(m))
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
		path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/chats?limit=100"
		var chats []core.Chat
		if err := c.do(ctx, http.MethodGet, path, nil, &chats); err != nil {
			return err
		}
		for _, chat := range chats {
			fmt.Printf("%s  %s  %s\n", chat.LastMessage.OccurredAt.Local().Format("2006-01-02 15:04"), chat.ID, messagePreview(chat.LastMessage))
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
		path := "/api/v1/accounts/" + url.PathEscape(account.ID) + "/chats/" + url.PathEscape(args[2]) + "/messages?limit=100"
		var messages []core.Message
		if err := c.do(ctx, http.MethodGet, path, nil, &messages); err != nil {
			return err
		}
		for i := len(messages) - 1; i >= 0; i-- {
			message := messages[i]
			fmt.Printf("%s  %-8s  %s\n", message.OccurredAt.Local().Format("2006-01-02 15:04"), message.Direction, messagePreview(message))
		}
		return nil
	default:
		return usage()
	}
}

func messagePreview(message core.Message) string {
	if message.Kind == core.MessageKindText || message.Kind == "" {
		return message.Text
	}
	if message.Text == "" {
		return "[" + string(message.Kind) + "]"
	}
	return "[" + string(message.Kind) + "] " + message.Text
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
	return errors.New("usage: convomeow [--data-dir DIR] serve | account add NAME | account list | account login NAME | chat list ACCOUNT | chat messages ACCOUNT CHAT | message send ACCOUNT RECIPIENT TEXT | message list ACCOUNT")
}
