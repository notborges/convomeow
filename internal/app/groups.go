package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/notborges/convomeow/internal/core"
)

const (
	maxGroupName         = 100
	maxGroupDescription  = 2048
	maxGroupParticipants = 256
)

func (s *Service) CreateGroup(ctx context.Context, accountID, name string, participants []string) (core.Group, error) {
	name, err := validGroupName(name)
	if err != nil {
		return core.Group{}, err
	}
	if len(participants) > maxGroupParticipants {
		return core.Group{}, fmt.Errorf("%w: add at most %d participants at a time", core.ErrInvalid, maxGroupParticipants)
	}
	manager, err := s.groupManager(accountID)
	if err != nil {
		return core.Group{}, err
	}
	group, err := manager.CreateGroup(ctx, name, participants)
	if err != nil {
		return core.Group{}, err
	}
	profile := core.ChatProfile{ProviderChatID: group.ProviderChatID, Kind: "group", DisplayName: &group.Name, Description: &group.Description, CreateConversation: true}
	if err := s.repo.UpdateChatProfile(ctx, accountID, profile); err != nil {
		return core.Group{}, err
	}
	group.ConversationID, err = s.repo.FindConversationID(ctx, accountID, group.ProviderChatID)
	if err != nil {
		return core.Group{}, err
	}
	s.describeGroup(ctx, accountID, &group)
	s.notifyConversation(accountID, group.ConversationID)
	return group, nil
}

func (s *Service) Group(ctx context.Context, conversationID string) (core.Group, error) {
	conversation, manager, err := s.groupConversation(ctx, conversationID)
	if err != nil {
		return core.Group{}, err
	}
	group, err := manager.Group(ctx, conversation.ProviderChatID)
	if err != nil {
		return core.Group{}, err
	}
	group.ConversationID = conversation.ID
	s.describeGroup(ctx, conversation.AccountID, &group)
	return group, nil
}

func (s *Service) UpdateGroup(ctx context.Context, conversationID string, update core.GroupUpdate) (core.Group, error) {
	if update.Name == nil && update.Description == nil && update.AdminsOnlySend == nil && update.AdminsOnlyEdit == nil {
		return core.Group{}, fmt.Errorf("%w: provide at least one group setting", core.ErrInvalid)
	}
	if update.Name != nil {
		name, err := validGroupName(*update.Name)
		if err != nil {
			return core.Group{}, err
		}
		update.Name = &name
	}
	if update.Description != nil {
		description := strings.TrimSpace(*update.Description)
		if utf8.RuneCountInString(description) > maxGroupDescription {
			return core.Group{}, fmt.Errorf("%w: description must be at most %d characters", core.ErrInvalid, maxGroupDescription)
		}
		update.Description = &description
	}
	conversation, manager, err := s.groupConversation(ctx, conversationID)
	if err != nil {
		return core.Group{}, err
	}
	if err := manager.UpdateGroup(ctx, conversation.ProviderChatID, update); err != nil {
		return core.Group{}, err
	}
	if update.Name != nil || update.Description != nil {
		profile := core.ChatProfile{ProviderChatID: conversation.ProviderChatID, Kind: "group", DisplayName: update.Name, Description: update.Description}
		if err := s.repo.UpdateChatProfile(ctx, conversation.AccountID, profile); err != nil {
			return core.Group{}, err
		}
	}
	s.notifyConversation(conversation.AccountID, conversation.ID)
	return s.Group(ctx, conversationID)
}

func (s *Service) ChangeGroupParticipants(ctx context.Context, conversationID string, action core.GroupParticipantAction, participants []string) ([]core.GroupParticipantResult, error) {
	switch action {
	case core.GroupParticipantAdd, core.GroupParticipantRemove, core.GroupParticipantPromote, core.GroupParticipantDemote:
	default:
		return nil, fmt.Errorf("%w: action must be add, remove, promote or demote", core.ErrInvalid)
	}
	if len(participants) == 0 || len(participants) > maxGroupParticipants {
		return nil, fmt.Errorf("%w: provide 1-%d participants", core.ErrInvalid, maxGroupParticipants)
	}
	conversation, manager, err := s.groupConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	results, err := manager.ChangeGroupParticipants(ctx, conversation.ProviderChatID, action, participants)
	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []core.GroupParticipantResult{}
	}
	names := s.participantNames(conversation.AccountID)
	for i := range results {
		results[i].DisplayName = names(ctx, results[i].ProviderID)
	}
	s.notifyConversation(conversation.AccountID, conversation.ID)
	return results, nil
}

func (s *Service) LeaveGroup(ctx context.Context, conversationID string) error {
	conversation, manager, err := s.groupConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := manager.LeaveGroup(ctx, conversation.ProviderChatID); err != nil {
		return err
	}
	s.notifyConversation(conversation.AccountID, conversation.ID)
	return nil
}

func (s *Service) GroupInviteLink(ctx context.Context, conversationID string, reset bool) (string, error) {
	conversation, manager, err := s.groupConversation(ctx, conversationID)
	if err != nil {
		return "", err
	}
	return manager.GroupInviteLink(ctx, conversation.ProviderChatID, reset)
}

func (s *Service) groupManager(accountID string) (core.GroupManager, error) {
	if err := s.requireCapability("manage_groups"); err != nil {
		return nil, err
	}
	rt, err := s.runtime(accountID)
	if err != nil {
		return nil, err
	}
	rt.mu.RLock()
	session, state := rt.session, rt.state
	rt.mu.RUnlock()
	if state != "connected" || session == nil {
		return nil, core.ErrNotConnected
	}
	manager, ok := session.(core.GroupManager)
	if !ok {
		return nil, fmt.Errorf("%w: manage_groups", core.ErrUnsupported)
	}
	return manager, nil
}

func (s *Service) groupConversation(ctx context.Context, conversationID string) (core.Conversation, core.GroupManager, error) {
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return core.Conversation{}, nil, err
	}
	if conversation.Kind != "group" {
		return core.Conversation{}, nil, fmt.Errorf("%w: conversation is not a group", core.ErrInvalid)
	}
	manager, err := s.groupManager(conversation.AccountID)
	return conversation, manager, err
}

func (s *Service) describeGroup(ctx context.Context, accountID string, group *core.Group) {
	names := s.participantNames(accountID)
	for i := range group.Participants {
		group.Participants[i].DisplayName = names(ctx, group.Participants[i].ProviderID)
	}
	for i := range group.FailedParticipants {
		group.FailedParticipants[i].DisplayName = names(ctx, group.FailedParticipants[i].ProviderID)
	}
	rank := map[core.GroupRole]int{core.GroupRoleOwner: 0, core.GroupRoleAdmin: 1, core.GroupRoleMember: 2}
	sort.SliceStable(group.Participants, func(i, j int) bool {
		a, b := group.Participants[i], group.Participants[j]
		if a.IsSelf != b.IsSelf {
			return a.IsSelf
		}
		if rank[a.Role] != rank[b.Role] {
			return rank[a.Role] < rank[b.Role]
		}
		return strings.ToLower(a.DisplayName) < strings.ToLower(b.DisplayName)
	})
}

// participantNames returns a lookup that caches results, including misses, for one request.
func (s *Service) participantNames(accountID string) func(context.Context, string) string {
	cache := map[string]string{}
	return func(ctx context.Context, providerID string) string {
		if name, ok := cache[providerID]; ok {
			return name
		}
		name := ""
		if contact, err := s.Contact(ctx, accountID, providerID); err == nil {
			name = contact.Name
		}
		cache[providerID] = name
		return name
	}
}

func (s *Service) withSenderNames(ctx context.Context, messages []core.Message) {
	lookups := map[string]func(context.Context, string) string{}
	for i := range messages {
		m := &messages[i]
		names := lookups[m.AccountID]
		if names == nil {
			names = s.participantNames(m.AccountID)
			lookups[m.AccountID] = names
		}
		if m.Direction == "inbound" && m.SenderID != "" {
			m.SenderName = names(ctx, m.SenderID)
		}
		if m.Reply != nil && m.Reply.SenderID != "" {
			m.Reply.SenderName = names(ctx, m.Reply.SenderID)
		}
	}
}

func validGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxGroupName {
		return "", fmt.Errorf("%w: group name must be 1-%d characters", core.ErrInvalid, maxGroupName)
	}
	return name, nil
}
