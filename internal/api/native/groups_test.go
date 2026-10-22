package native_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/core"
)

func (s *fakeSession) CreateGroup(_ context.Context, name string, participants []string) (core.Group, error) {
	c := s.connector
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	c.group = core.Group{ProviderChatID: "team@g.us", Name: name, OwnRole: core.GroupRoleOwner,
		Participants: []core.GroupParticipant{{ProviderID: "333@s.whatsapp.net", Role: core.GroupRoleMember}, {ProviderID: "self@s.whatsapp.net", Role: core.GroupRoleOwner, IsSelf: true}}}
	for _, participant := range participants {
		if participant == "222" {
			c.group.FailedParticipants = append(c.group.FailedParticipants, core.GroupParticipantResult{ProviderID: "222@s.whatsapp.net", Status: "privacy_restricted"})
			continue
		}
		c.group.Participants = append(c.group.Participants, core.GroupParticipant{ProviderID: participant, Role: core.GroupRoleAdmin})
	}
	return c.group, nil
}

func (s *fakeSession) Group(_ context.Context, chatID string) (core.Group, error) {
	c := s.connector
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	if c.groupErr != nil {
		return core.Group{}, c.groupErr
	}
	if chatID != c.group.ProviderChatID {
		return core.Group{}, core.ErrNotFound
	}
	return c.group, nil
}

func (s *fakeSession) UpdateGroup(_ context.Context, _ string, update core.GroupUpdate) error {
	c := s.connector
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	c.groupUpdates = append(c.groupUpdates, update)
	if update.Name != nil {
		c.group.Name = *update.Name
	}
	if update.AdminsOnlySend != nil {
		c.group.AdminsOnlySend = *update.AdminsOnlySend
	}
	return nil
}

func (s *fakeSession) ChangeGroupParticipants(_ context.Context, _ string, _ core.GroupParticipantAction, participants []string) ([]core.GroupParticipantResult, error) {
	results := make([]core.GroupParticipantResult, 0, len(participants))
	for _, participant := range participants {
		results = append(results, core.GroupParticipantResult{ProviderID: participant, Status: "ok"})
	}
	return results, nil
}

func (s *fakeSession) LeaveGroup(context.Context, string) error {
	s.connector.groupMu.Lock()
	s.connector.leftGroups++
	s.connector.groupMu.Unlock()
	return nil
}

func (s *fakeSession) GroupInviteLink(_ context.Context, _ string, reset bool) (string, error) {
	s.connector.groupMu.Lock()
	defer s.connector.groupMu.Unlock()
	if reset {
		s.connector.inviteResets++
	}
	return "https://chat.whatsapp.com/code", nil
}

func TestGroupManagement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{contacts: map[string]core.Contact{
		"111@s.whatsapp.net": {ProviderID: "111@s.whatsapp.net", Name: "Alice"},
		"333@s.whatsapp.net": {ProviderID: "333@s.whatsapp.net", Name: "Bruno"},
	}}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	waitFor(t, func() bool {
		status, err := service.Account(account)
		return err == nil && status.State == "connected"
	})
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	client := server.Client()
	type problem struct {
		Code string `json:"code"`
	}

	for _, body := range []map[string]any{{"name": "  "}, {"name": strings.Repeat("a", 101)}} {
		res := request(t, client, "POST", server.URL+"/api/v1/accounts/"+account+"/groups", body, "")
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("invalid group name accepted: %d", res.StatusCode)
		}
	}
	created := decode[core.Group](t, request(t, client, "POST", server.URL+"/api/v1/accounts/"+account+"/groups",
		map[string]any{"name": "  Team  ", "participants": []string{"111@s.whatsapp.net", "222"}}, ""), 201)
	if created.ConversationID == "" || created.Name != "Team" || len(created.Participants) != 3 {
		t.Fatalf("created group: %+v", created)
	}
	if !created.Participants[0].IsSelf || created.Participants[1].DisplayName != "Alice" || created.Participants[2].DisplayName != "Bruno" {
		t.Fatalf("participant order or names: %+v", created.Participants)
	}
	if len(created.FailedParticipants) != 1 || created.FailedParticipants[0].Status != "privacy_restricted" {
		t.Fatalf("failed participants: %+v", created.FailedParticipants)
	}
	conversation, err := store.GetConversation(context.Background(), created.ConversationID)
	if err != nil || conversation.Kind != "group" || conversation.DisplayName != "Team" {
		t.Fatalf("group conversation: %+v %v", conversation, err)
	}

	groupURL := server.URL + "/api/v1/conversations/" + created.ConversationID + "/group"
	decode[core.Group](t, request(t, client, "GET", groupURL, nil, ""), 200)
	res := request(t, client, "PATCH", groupURL, map[string]any{}, "")
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("empty update: %d", res.StatusCode)
	}
	updated := decode[core.Group](t, request(t, client, "PATCH", groupURL, map[string]any{"name": "Launch", "admins_only_send": true}, ""), 200)
	if updated.Name != "Launch" || !updated.AdminsOnlySend {
		t.Fatalf("updated group: %+v", updated)
	}
	conversation, err = store.GetConversation(context.Background(), created.ConversationID)
	if err != nil || conversation.DisplayName != "Launch" {
		t.Fatalf("local name not updated: %+v %v", conversation, err)
	}

	res = request(t, client, "POST", groupURL+"/participants", map[string]any{"action": "kick", "participants": []string{"111@s.whatsapp.net"}}, "")
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("unknown action: %d", res.StatusCode)
	}
	results := decode[testPage[core.GroupParticipantResult]](t, request(t, client, "POST", groupURL+"/participants",
		map[string]any{"action": "remove", "participants": []string{"111@s.whatsapp.net"}}, ""), 200)
	if len(results.Items) != 1 || results.Items[0].Status != "ok" || results.Items[0].DisplayName != "Alice" {
		t.Fatalf("participant results: %+v", results.Items)
	}

	link := decode[map[string]string](t, request(t, client, "GET", groupURL+"/invite-link", nil, ""), 200)
	decode[map[string]string](t, request(t, client, "POST", groupURL+"/invite-link", nil, ""), 200)
	if link["url"] != "https://chat.whatsapp.com/code" || connector.inviteResets != 1 {
		t.Fatalf("invite link: %v resets=%d", link, connector.inviteResets)
	}
	res = request(t, client, "POST", groupURL+"/leave", nil, "")
	res.Body.Close()
	if res.StatusCode != 204 || connector.leftGroups != 1 {
		t.Fatalf("leave: %d", res.StatusCode)
	}

	connector.groupMu.Lock()
	connector.groupErr = core.ErrNotParticipant
	connector.groupMu.Unlock()
	if p := decode[problem](t, request(t, client, "GET", groupURL, nil, ""), 403); p.Code != "not_group_participant" {
		t.Fatalf("not participant: %+v", p)
	}

	direct, err := store.SaveMessage(context.Background(), core.Message{AccountID: account, ChatID: "111@s.whatsapp.net", SenderID: "111@s.whatsapp.net", ProviderMessageID: "d1", Direction: "inbound", State: "received", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	res = request(t, client, "GET", server.URL+"/api/v1/conversations/"+direct.ConversationID+"/group", nil, "")
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("direct conversation as group: %d", res.StatusCode)
	}
}

func TestGroupSenderNames(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.sqlite")
	account := uuid.NewString()
	createAvatarTestAccount(t, path, account)
	connector := &fakeConnector{contacts: map[string]core.Contact{"111@s.whatsapp.net": {ProviderID: "111@s.whatsapp.net", Name: "Alice"}}}
	service, store := avatarTestService(t, path, filepath.Join(root, "media"), connector)
	defer service.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	first, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: "team@g.us", SenderID: "111@s.whatsapp.net", ProviderMessageID: "g1", Direction: "inbound", State: "received", Kind: core.MessageKindText, Text: "hi", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := store.SaveMessage(ctx, core.Message{AccountID: account, ChatID: "team@g.us", SenderID: "999@s.whatsapp.net", ProviderMessageID: "g2", Direction: "inbound", State: "received", Kind: core.MessageKindText, Text: "hello",
		Reply: &core.Reply{ProviderMessageID: "g1", SenderID: "111@s.whatsapp.net"}, OccurredAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	type message struct {
		ID         string     `json:"id"`
		SenderName string     `json:"sender_name"`
		Reply      core.Reply `json:"reply"`
	}
	page := decode[testPage[message]](t, request(t, server.Client(), "GET", server.URL+"/api/v1/conversations/"+first.ConversationID+"/messages", nil, ""), 200)
	names := map[string]message{}
	for _, item := range page.Items {
		names[item.ID] = item
	}
	if names[first.ID].SenderName != "Alice" || names[reply.ID].SenderName != "" || names[reply.ID].Reply.SenderName != "Alice" {
		t.Fatalf("sender names: %+v", page.Items)
	}
	single := decode[message](t, request(t, server.Client(), "GET", server.URL+"/api/v1/messages/"+first.ID, nil, ""), 200)
	if single.SenderName != "Alice" {
		t.Fatalf("single message sender name: %+v", single)
	}
}
