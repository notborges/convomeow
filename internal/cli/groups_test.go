package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestGroupParticipantCommand(t *testing.T) {
	var body struct {
		Action       string   `json:"action"`
		Participants []string `json:"participants"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/versions":
			_ = json.NewEncoder(w).Encode(map[string][]string{"versions": {"v1"}})
		case "/api/v1/conversations/chat-1/group/participants":
			if r.Method != http.MethodPost {
				t.Errorf("method: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]string{{"provider_id": "111@s.whatsapp.net", "status": "ok"}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := New(server.URL, "secret")
	if err := client.Run(context.Background(), []string{"group", "promote", "chat-1", "+111", "222@s.whatsapp.net"}); err != nil {
		t.Fatal(err)
	}
	if body.Action != "promote" || !slices.Equal(body.Participants, []string{"+111", "222@s.whatsapp.net"}) {
		t.Fatalf("request body: %+v", body)
	}
	if err := client.Run(context.Background(), []string{"group", "admins-only", "chat-1", "send", "maybe"}); err == nil {
		t.Fatal("invalid setting accepted")
	}
}
