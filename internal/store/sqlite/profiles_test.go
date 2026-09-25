package sqlite

import (
	"context"
	"testing"

	"github.com/notborges/convomeow/internal/core"
)

func TestChatProfilesStayInTheirAccountAndSurviveAliasMerge(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	pn, lid := "15551234567@s.whatsapp.net", "abc123@lid"
	primary, _, err := store.GetOrCreateConversation(ctx, "account-1", pn)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _, err := store.GetOrCreateConversation(ctx, "account-1", lid)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := store.GetOrCreateConversation(ctx, "account-2", pn)
	if err != nil {
		t.Fatal(err)
	}
	name := "Alice"
	if err := store.UpdateChatProfile(ctx, "account-1", core.ChatProfile{ProviderChatID: lid, AlternateID: pn, Kind: "direct", DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	merged, err := store.GetConversation(ctx, primary.ID)
	if err != nil || merged.DisplayName != name || merged.Kind != "direct" {
		t.Fatalf("merged profile: %+v, %v", merged, err)
	}
	if _, err := store.GetConversation(ctx, duplicate.ID); err == nil {
		t.Fatal("duplicate conversation survived alias merge")
	}
	isolated, err := store.GetConversation(ctx, other.ID)
	if err != nil || isolated.DisplayName == name {
		t.Fatalf("other account profile changed: %+v, %v", isolated, err)
	}
}

func TestJoinedGroupProfileCreatesConversationAndUpdatesDescription(t *testing.T) {
	ctx := context.Background()
	store, _ := historyTestStore(t)
	defer store.Close()
	name, description := "Family", "Weekend plans"
	profile := core.ChatProfile{ProviderChatID: "group@g.us", Kind: "group", DisplayName: &name,
		Description: &description, CreateConversation: true}
	if err := store.UpdateChatProfile(ctx, "account-1", profile); err != nil {
		t.Fatal(err)
	}
	conversations, err := store.ListConversations(ctx, "account-1", nil, 10)
	if err != nil || len(conversations) != 1 || conversations[0].Kind != "group" || conversations[0].DisplayName != name || conversations[0].Description != description {
		t.Fatalf("group profile: %+v, %v", conversations, err)
	}
	empty := ""
	if err := store.UpdateChatProfile(ctx, "account-1", core.ChatProfile{ProviderChatID: "group@g.us", Description: &empty}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetConversation(ctx, conversations[0].ID)
	if err != nil || updated.Description != "" || updated.DisplayName != name {
		t.Fatalf("cleared group description: %+v, %v", updated, err)
	}
}
