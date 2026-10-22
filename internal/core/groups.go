package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden      = errors.New("permission denied")
	ErrNotParticipant = errors.New("account is not a participant in this group")
)

type GroupRole string

const (
	GroupRoleMember GroupRole = "member"
	GroupRoleAdmin  GroupRole = "admin"
	GroupRoleOwner  GroupRole = "owner"
)

type GroupParticipantAction string

const (
	GroupParticipantAdd     GroupParticipantAction = "add"
	GroupParticipantRemove  GroupParticipantAction = "remove"
	GroupParticipantPromote GroupParticipantAction = "promote"
	GroupParticipantDemote  GroupParticipantAction = "demote"
)

type Group struct {
	ConversationID     string                   `json:"conversation_id"`
	ProviderChatID     string                   `json:"provider_chat_id"`
	Name               string                   `json:"name"`
	Description        string                   `json:"description,omitempty"`
	CreatedAt          *time.Time               `json:"created_at,omitempty"`
	AdminsOnlySend     bool                     `json:"admins_only_send"`
	AdminsOnlyEdit     bool                     `json:"admins_only_edit"`
	OwnRole            GroupRole                `json:"own_role"`
	Participants       []GroupParticipant       `json:"participants"`
	FailedParticipants []GroupParticipantResult `json:"failed_participants,omitempty"`
}

type GroupParticipant struct {
	ProviderID  string    `json:"provider_id"`
	AlternateID string    `json:"-"`
	DisplayName string    `json:"display_name,omitempty"`
	Phone       string    `json:"phone,omitempty"`
	Role        GroupRole `json:"role"`
	IsSelf      bool      `json:"is_self"`
}

// GroupParticipantResult reports one participant's outcome; providers can reject
// individual participants while accepting the rest of the request.
type GroupParticipantResult struct {
	ProviderID  string `json:"provider_id"`
	DisplayName string `json:"display_name,omitempty"`
	Status      string `json:"status"`
}

type GroupUpdate struct {
	Name           *string
	Description    *string
	AdminsOnlySend *bool
	AdminsOnlyEdit *bool
}

type GroupManager interface {
	CreateGroup(ctx context.Context, name string, participants []string) (Group, error)
	Group(ctx context.Context, chatID string) (Group, error)
	UpdateGroup(ctx context.Context, chatID string, update GroupUpdate) error
	ChangeGroupParticipants(ctx context.Context, chatID string, action GroupParticipantAction, participants []string) ([]GroupParticipantResult, error)
	LeaveGroup(ctx context.Context, chatID string) error
	GroupInviteLink(ctx context.Context, chatID string, reset bool) (string, error)
}
