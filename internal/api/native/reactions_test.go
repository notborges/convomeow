package native_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/core"
)

func (s *fakeSession) SendReaction(_ context.Context, m core.Message, emoji string, at time.Time) (core.Reaction, error) {
	r := core.Reaction{ChatID: m.ChatID, TargetID: m.ProviderMessageID, Emoji: emoji, At: at, EventID: uuid.NewString(), IsOwn: true}
	if s.connector.reactionCalls != nil {
		s.connector.reactionCalls <- r
	}
	if s.connector.failReaction.Load() {
		return core.Reaction{}, core.ErrReactionUnconfirmed
	}
	return r, nil
}

func TestReactionCommandsAndIncomingUpdates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{reactionCalls: make(chan core.Reaction, 10)}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	target, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: "123@s.whatsapp.net", SenderID: "123@s.whatsapp.net", ProviderMessageID: "m1", Direction: "inbound"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: target.ChatID, ProviderMessageID: "m2", Direction: "outbound", State: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	url := server.URL + "/api/v1/messages/" + target.ID + "/reaction"
	for _, body := range []any{map[string]string{"emoji": "hello"}, map[string]string{"emoji": ""}, map[string]string{"emoji": "👍👍"}, map[string]string{"emoji": "👍", "participant_id": "other"}} {
		res := request(t, server.Client(), "PUT", url, body, "")
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("bad input accepted: %d", res.StatusCode)
		}
	}
	res := request(t, server.Client(), "PUT", server.URL+"/api/v1/messages/"+queued.ID+"/reaction", map[string]string{"emoji": "👍"}, "")
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
	req, _ := http.NewRequest("DELETE", url, nil)
	res, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("unauthenticated mutation", res.StatusCode)
	}
	if len(connector.reactionCalls) != 0 {
		t.Fatal("invalid request sent reaction")
	}
	got := decode[core.Message](t, request(t, server.Client(), "PUT", url, map[string]string{"emoji": "👍🏽"}, ""), 200)
	if len(got.Reactions) != 1 || !got.Reactions[0].Own || got.Reactions[0].Emoji != "👍🏽" {
		t.Fatalf("response: %+v", got.Reactions)
	}
	decode[core.Message](t, request(t, server.Client(), "PUT", url, map[string]string{"emoji": "👍🏽"}, ""), 200)
	if len(connector.reactionCalls) != 1 {
		t.Fatal("identical state resent")
	}
	decode[core.Message](t, request(t, server.Client(), "PUT", url, map[string]string{"emoji": "❤️"}, ""), 200)
	first, second := <-connector.reactionCalls, <-connector.reactionCalls
	if !second.At.After(first.At) {
		t.Fatal("command timestamps not ordered")
	}
	connector.failReaction.Store(true)
	res = request(t, server.Client(), "PUT", url, map[string]string{"emoji": "🔥"}, "")
	res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatal(res.StatusCode)
	}
	got, err = store.GetMessage(ctx, target.ID)
	if err != nil || got.Reactions[0].Emoji != "❤️" {
		t.Fatal("failed reaction changed saved state", err)
	}
	connector.failReaction.Store(false)
	got = decode[core.Message](t, request(t, server.Client(), "DELETE", url, nil, ""), 200)
	if len(got.Reactions) != 0 {
		t.Fatal("removal not reflected")
	}
	other := core.Reaction{ChatID: target.ChatID, TargetID: "m1", ParticipantID: "123@s.whatsapp.net", Emoji: "😂", At: time.Now(), EventID: "phone"}
	connector.emitEvent(core.Event{Type: core.EventReaction, Reaction: &other})
	got = decode[core.Message](t, request(t, server.Client(), "GET", server.URL+"/api/v1/messages/"+target.ID, nil, ""), 200)
	if len(got.Reactions) != 1 || got.Reactions[0].Own || got.Reactions[0].Emoji != "😂" {
		t.Fatalf("incoming: %+v", got.Reactions)
	}
	type page struct {
		Items []core.MessageReaction `json:"items"`
	}
	details := decode[page](t, request(t, server.Client(), "GET", url+"s", nil, ""), 200)
	if len(details.Items) != 1 || details.Items[0].IsOwn {
		t.Fatal(details)
	}
	connector.emitEvent(core.Event{Type: core.EventDisconnected})
	res = request(t, server.Client(), "PUT", url, map[string]string{"emoji": "👍"}, "")
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal("offline", res.StatusCode)
	}
}
