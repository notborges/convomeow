package v1

import (
	"slices"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type accountResponse struct {
	AvatarURL        string    `json:"avatar_url"`
	ID               string    `json:"id"`
	Provider         string    `json:"provider"`
	ConnectionKind   string    `json:"connection_kind"`
	Label            string    `json:"label"`
	ProviderIdentity string    `json:"provider_identity,omitempty"`
	State            string    `json:"state"`
	LastError        string    `json:"last_error,omitempty"`
	Capabilities     []string  `json:"capabilities"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func accountFromCore(a core.AccountStatus) accountResponse {
	avatar := ""
	if slices.Contains(a.Capabilities, "read_avatars") {
		avatar = "/api/v1/accounts/" + a.ID + "/avatar"
	}
	return accountResponse{AvatarURL: avatar, ID: a.ID, Provider: a.Provider, ConnectionKind: a.ConnectionKind, Label: a.Label,
		ProviderIdentity: a.ProviderIdentity, State: a.State, LastError: a.LastError,
		Capabilities: a.Capabilities, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

type messageResponse struct {
	Actions           core.MessageActions    `json:"actions"`
	EditedAt          *time.Time             `json:"edited_at,omitempty"`
	DeletedAt         *time.Time             `json:"deleted_at,omitempty"`
	Reactions         []core.ReactionSummary `json:"reactions,omitempty"`
	ReadAt            *time.Time             `json:"read_at,omitempty"`
	Delivery          *core.DeliverySummary  `json:"delivery,omitempty"`
	Reply             *core.Reply            `json:"reply,omitempty"`
	ID                string                 `json:"id"`
	AccountID         string                 `json:"account_id"`
	ConversationID    string                 `json:"conversation_id"`
	ProviderMessageID string                 `json:"provider_message_id,omitempty"`
	Direction         string                 `json:"direction"`
	State             string                 `json:"state"`
	SenderID          string                 `json:"sender_id,omitempty"`
	Kind              string                 `json:"kind"`
	Content           any                    `json:"content"`
	Attachments       []attachmentResponse   `json:"attachments,omitempty"`
	OccurredAt        time.Time              `json:"occurred_at"`
	IngestedAt        time.Time              `json:"ingested_at"`
}

type attachmentResponse struct {
	DurationSeconds uint32 `json:"duration_seconds,omitempty"`
	Width           uint32 `json:"width,omitempty"`
	Height          uint32 `json:"height,omitempty"`
	AttemptCount    int    `json:"attempt_count,omitempty"`
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	MIMEType        string `json:"mime_type,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	Size            uint64 `json:"size,omitempty"`
	Availability    string `json:"availability"`
}

func (s *Server) messageFromCore(m core.Message) messageResponse {
	var content any = map[string]string{}
	if m.Kind == core.MessageKindText {
		content = map[string]string{"text": m.Text}
	} else if m.Text != "" {
		content = map[string]string{"caption": m.Text}
	}
	attachments := make([]attachmentResponse, 0, len(m.Attachments))
	for _, attachment := range m.Attachments {
		attachments = append(attachments, attachmentResponse{ID: attachment.ID, Kind: string(attachment.Kind),
			DurationSeconds: attachment.DurationSeconds, Width: attachment.Width, Height: attachment.Height, MIMEType: attachment.MIMEType, FileName: attachment.FileName, Size: attachment.Size, Availability: attachment.Availability})
	}
	return messageResponse{Actions: s.service.MessageActions(m), EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, Reactions: m.Reactions, ReadAt: m.ReadAt, Delivery: m.Delivery, Reply: m.Reply, ID: m.ID, AccountID: m.AccountID, ConversationID: m.ConversationID,
		ProviderMessageID: m.ProviderMessageID, Direction: m.Direction, State: m.State, SenderID: m.SenderID,
		Kind: string(m.Kind), Content: content, Attachments: attachments, OccurredAt: m.OccurredAt, IngestedAt: m.IngestedAt}
}

func attachmentFromRecord(record core.MediaRecord) attachmentResponse {
	size := record.DeclaredSize
	if record.Availability == "ready" {
		size = uint64(record.StoredSize)
	}
	return attachmentResponse{ID: record.AttachmentID, Kind: string(record.Kind), MIMEType: record.MIMEType,
		DurationSeconds: record.DurationSeconds, Width: record.Width, Height: record.Height, FileName: record.FileName, Size: size, Availability: record.Availability, AttemptCount: record.AttemptCount}
}

type conversationResponse struct {
	ID             string           `json:"id"`
	AccountID      string           `json:"account_id"`
	ProviderChatID string           `json:"provider_chat_id"`
	Kind           string           `json:"kind"`
	DisplayName    string           `json:"display_name"`
	Description    string           `json:"description,omitempty"`
	AvatarURL      string           `json:"avatar_url"`
	Contact        *contactResponse `json:"contact,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	LastMessage    *messageResponse `json:"last_message,omitempty"`
}

func (s *Server) conversationFromCore(c core.Conversation) conversationResponse {
	result := conversationResponse{ID: c.ID, AccountID: c.AccountID, ProviderChatID: c.ProviderChatID,
		Kind: c.Kind, DisplayName: c.DisplayName, Description: c.Description, AvatarURL: s.avatarURL(c.AccountID, "/api/v1/conversations/"+c.ID+"/avatar"),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.Contact != nil {
		contact := s.contactFromCore(c.AccountID, *c.Contact)
		result.Contact = &contact
	}
	if c.LastMessage != nil {
		last := s.messageFromCore(*c.LastMessage)
		result.LastMessage = &last
	}
	return result
}

type pageResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type loginAttemptResponse struct {
	ID        string                  `json:"id"`
	State     string                  `json:"state"`
	Challenge *loginChallengeResponse `json:"challenge,omitempty"`
	Error     string                  `json:"error,omitempty"`
}

type loginChallengeResponse struct {
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

func loginAttemptFromCore(status core.LoginStatus) loginAttemptResponse {
	result := loginAttemptResponse{ID: status.ID, State: status.State, Error: status.Error}
	if status.Challenge != nil {
		result.Challenge = &loginChallengeResponse{Type: status.Challenge.Type, Value: status.Challenge.Value, ExpiresAt: status.Challenge.ExpiresAt}
	}
	return result
}

func (s *Server) avatarURL(accountID, path string) string {
	account, err := s.service.Account(accountID)
	if err != nil || !slices.Contains(account.Capabilities, "read_avatars") {
		return ""
	}
	return path
}
