package native_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/core"
)

func (s *fakeSession) EditMessage(_ context.Context, m core.Message, text string, at time.Time) (core.MessageChange, error) {
	return s.changeMessage(m, "edit", text, at)
}
func (s *fakeSession) RevokeMessage(_ context.Context, m core.Message, at time.Time) (core.MessageChange, error) {
	return s.changeMessage(m, "revoke", "", at)
}
func (s *fakeSession) changeMessage(m core.Message, kind, text string, at time.Time) (core.MessageChange, error) {
	c := core.MessageChange{ChatID: m.ChatID, TargetID: m.ProviderMessageID, Kind: kind, Text: text, At: at, EventID: uuid.NewString()}
	if s.connector.changeCalls != nil {
		s.connector.changeCalls <- c
	}
	if s.connector.failChange.Load() {
		return core.MessageChange{}, core.ErrMessageChangeUnconfirmed
	}
	return c, nil
}
func TestMessageChangesAPI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{changeCalls: make(chan core.MessageChange, 10)}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	at := time.Now().Add(-time.Minute)
	m, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: "123@s.whatsapp.net", ProviderMessageID: "target", Direction: "outbound", Text: "first", OccurredAt: at})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	base := server.URL + "/api/v1/messages/" + m.ID
	got := decode[core.Message](t, request(t, server.Client(), "PATCH", base, map[string]string{"text": "second"}, ""), 200)
	if got.EditedAt == nil || !got.OccurredAt.Equal(at) {
		t.Fatal(got)
	}
	connector.failChange.Store(true)
	res := request(t, server.Client(), "PATCH", base, map[string]string{"text": "failed"}, "")
	res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatal(res.StatusCode)
	}
	connector.failChange.Store(false)
	got = decode[core.Message](t, request(t, server.Client(), "POST", base+"/revoke", nil, ""), 200)
	if got.DeletedAt == nil {
		t.Fatal("missing deletion")
	}
	stored, err := store.GetMessage(ctx, m.ID)
	if err != nil || stored.Text != "second" {
		t.Fatal("saved text lost", stored, err)
	}
	res = request(t, server.Client(), "PATCH", base, map[string]string{"text": "after deletion"}, "")
	res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
	type revisions struct {
		Items []core.MessageRevision `json:"items"`
		Next  string                 `json:"next_cursor"`
	}
	var kinds []string
	cursor := ""
	for i := 0; i < 4; i++ {
		page := decode[revisions](t, request(t, server.Client(), "GET", base+"/revisions?limit=1&cursor="+url.QueryEscape(cursor), nil, ""), 200)
		if len(page.Items) != 1 {
			t.Fatal(page)
		}
		kinds = append(kinds, page.Items[0].Kind)
		cursor = page.Next
		if cursor == "" {
			break
		}
	}
	if len(kinds) != 3 || kinds[0] != "revoke" || kinds[1] != "edit" || kinds[2] != "original" {
		t.Fatal(kinds)
	}
	res = request(t, server.Client(), "GET", base+"/revisions?cursor=bad", nil, "")
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
}
func TestUnsupportedCapabilitiesRejectExistingActions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{capabilitiesOverride: []string{}, changeCalls: make(chan core.MessageChange, 1), reactionCalls: make(chan core.Reaction, 1)}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	m, err := store.SaveMessage(context.Background(), core.Message{AccountID: account, ChatID: "123@s.whatsapp.net", ProviderMessageID: "target", Direction: "outbound", Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	base := server.URL + "/api/v1/messages/" + m.ID
	for _, c := range []struct {
		method, path string
		body         any
	}{{"PATCH", base, map[string]string{"text": "new"}}, {"POST", base + "/revoke", nil}, {"PUT", base + "/reaction", map[string]string{"emoji": "👍"}}, {"POST", server.URL + "/api/v1/conversations/" + m.ConversationID + "/presence", map[string]string{"client_id": "test", "activity": "typing"}}} {
		res := request(t, server.Client(), c.method, c.path, c.body, "")
		res.Body.Close()
		if res.StatusCode != 422 {
			t.Fatalf("%s: %d", c.path, res.StatusCode)
		}
	}
	var result struct {
		Actions core.MessageActions `json:"actions"`
	}
	result = decode[struct {
		Actions core.MessageActions `json:"actions"`
	}](t, request(t, server.Client(), "GET", base, nil, ""), 200)
	if result.Actions.Edit || result.Actions.Reply || result.Actions.React || result.Actions.Revoke {
		t.Fatal(result.Actions)
	}
	if len(connector.changeCalls) != 0 || len(connector.reactionCalls) != 0 {
		t.Fatal("unsupported operation called provider")
	}
}
