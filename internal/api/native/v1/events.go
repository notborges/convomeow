package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("presence_account_id")
	if accountID != "" {
		if _, err := s.service.Account(accountID); err != nil {
			respondError(w, r, err)
			return
		}
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024)
	ctx := conn.CloseRead(r.Context())
	changes, unsubscribe := s.service.Subscribe()
	defer unsubscribe()
	if accountID != "" {
		release, err := s.service.WatchPresence(accountID)
		if err != nil {
			return
		}
		defer release()
	}
	write := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return conn.Write(writeCtx, websocket.MessageText, data)
	}
	if err := write(struct {
		Type string `json:"type"`
	}{"ready"}); err != nil {
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-s.service.Done():
			_ = conn.Close(websocket.StatusGoingAway, "Service stopping")
			return
		case <-ctx.Done():
			return
		case change, ok := <-changes:
			if !ok {
				_ = conn.Close(websocket.StatusTryAgainLater, "Reconnect to refresh state")
				return
			}
			if !s.authorized(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "Session expired")
				return
			}
			if err := write(change); err != nil {
				return
			}
		case <-heartbeat.C:
			if !s.authorized(r) {
				_ = conn.Close(websocket.StatusPolicyViolation, "Session expired")
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
