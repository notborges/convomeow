package native_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func TestContactsIncludePeopleWithoutChatsAndEnrichConversations(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := uuid.NewString()
	now := time.Now().UTC()
	if err := store.CreateAccount(ctx, core.Account{ID: accountID, Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "sales", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetIdentity(ctx, accountID, "fake-device"); err != nil {
		t.Fatal(err)
	}
	groupID := "12345@g.us"
	if err := store.ImportHistory(ctx, core.HistoryBatch{AccountID: accountID, Chat: &core.HistoryChat{
		ID: groupID, Kind: "group", DisplayName: "Family", Description: "Weekend plans", LastActivityAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	photo := []byte("avatar-bytes")
	connector := &fakeConnector{contacts: map[string]core.Contact{
		"111@s.whatsapp.net": {ProviderID: "111@s.whatsapp.net", Name: "Alice", Phone: "+111"},
		"222@lid":            {ProviderID: "222@lid", Name: "Bob"},
	}, avatar: core.Avatar{ContentType: "image/jpeg", Data: photo}}
	service := app.New(store, connector, nil)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	client := server.Client()
	for attempt := 0; attempt < 50; attempt++ {
		account := decode[testAccount](t, request(t, client, http.MethodGet, server.URL+"/api/v1/accounts/"+accountID, nil, ""), http.StatusOK)
		if account.State == "connected" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	contactsURL := server.URL + "/api/v1/accounts/" + accountID + "/contacts"
	first := decode[testPage[struct {
		Name       string `json:"name"`
		ProviderID string `json:"provider_id"`
		AvatarURL  string `json:"avatar_url"`
	}]](t, request(t, client, http.MethodGet, contactsURL+"?limit=1", nil, ""), http.StatusOK)
	if len(first.Items) != 1 || first.Items[0].Name != "Alice" || first.NextCursor == "" {
		t.Fatalf("first contact page: %+v", first)
	}
	second := decode[testPage[struct {
		Name string `json:"name"`
	}]](t, request(t, client, http.MethodGet, contactsURL+"?limit=1&cursor="+url.QueryEscape(first.NextCursor), nil, ""), http.StatusOK)
	if len(second.Items) != 1 || second.Items[0].Name != "Bob" || second.NextCursor != "" {
		t.Fatalf("second contact page: %+v", second)
	}
	filtered := decode[testPage[struct {
		Name string `json:"name"`
	}]](t, request(t, client, http.MethodGet, contactsURL+"?q=bob", nil, ""), http.StatusOK)
	if len(filtered.Items) != 1 || filtered.Items[0].Name != "Bob" {
		t.Fatalf("contact search: %+v", filtered)
	}
	contactURL := contactsURL + "/222@lid"
	contact := decode[struct {
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}](t, request(t, client, http.MethodGet, contactURL, nil, ""), http.StatusOK)
	if contact.Name != "Bob" || contact.AvatarURL == "" {
		t.Fatalf("contact detail: %+v", contact)
	}
	decode[map[string]any](t, request(t, client, http.MethodGet, contactsURL+"/missing@lid", nil, ""), http.StatusNotFound)
	avatarResponse := request(t, client, http.MethodGet, server.URL+contact.AvatarURL, nil, "")
	defer avatarResponse.Body.Close()
	avatarBytes, err := io.ReadAll(avatarResponse.Body)
	if err != nil || avatarResponse.StatusCode != http.StatusOK || avatarResponse.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(avatarBytes, photo) {
		t.Fatalf("contact avatar: status=%d content_type=%s bytes=%q error=%v", avatarResponse.StatusCode, avatarResponse.Header.Get("Content-Type"), avatarBytes, err)
	}
	conversation := decode[struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Kind        string `json:"kind"`
		AvatarURL   string `json:"avatar_url"`
		Contact     struct {
			Name string `json:"name"`
		} `json:"contact"`
	}](t, request(t, client, http.MethodPost, server.URL+"/api/v1/accounts/"+accountID+"/conversations",
		map[string]any{"target": map[string]string{"type": "contact", "value": "222@lid"}}, ""), http.StatusCreated)
	if conversation.DisplayName != "Bob" || conversation.Kind != "direct" || conversation.Contact.Name != "Bob" {
		t.Fatalf("contact conversation: %+v", conversation)
	}
	avatarResponse = request(t, client, http.MethodGet, server.URL+conversation.AvatarURL, nil, "")
	avatarBytes, err = io.ReadAll(avatarResponse.Body)
	avatarResponse.Body.Close()
	if err != nil || avatarResponse.StatusCode != http.StatusOK || !bytes.Equal(avatarBytes, photo) {
		t.Fatalf("conversation avatar: status=%d bytes=%q error=%v", avatarResponse.StatusCode, avatarBytes, err)
	}
	conversations := decode[testPage[struct {
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
	}]](t, request(t, client, http.MethodGet, server.URL+"/api/v1/conversations?account_id="+accountID, nil, ""), http.StatusOK)
	foundGroup := false
	for _, item := range conversations.Items {
		if item.Kind == "group" {
			foundGroup = item.DisplayName == "Family" && item.Description == "Weekend plans"
		}
	}
	if !foundGroup {
		t.Fatalf("group profile missing from conversations: %+v", conversations)
	}
}
