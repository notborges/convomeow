package v1

import (
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type accountResponse struct {
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
	return accountResponse{ID: a.ID, Provider: a.Provider, ConnectionKind: a.ConnectionKind, Label: a.Label,
		ProviderIdentity: a.ProviderIdentity, State: a.State, LastError: a.LastError,
		Capabilities: []string{"read_messages", "read_media", "read_contacts", "read_avatars", "send_text", "send_media", "start_conversation"}, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

type messageResponse struct {
	ID                string               `json:"id"`
	AccountID         string               `json:"account_id"`
	ConversationID    string               `json:"conversation_id"`
	ProviderMessageID string               `json:"provider_message_id,omitempty"`
	Direction         string               `json:"direction"`
	State             string               `json:"state"`
	SenderID          string               `json:"sender_id,omitempty"`
	Kind              string               `json:"kind"`
	Content           any                  `json:"content"`
	Attachments       []attachmentResponse `json:"attachments,omitempty"`
	OccurredAt        time.Time            `json:"occurred_at"`
	IngestedAt        time.Time            `json:"ingested_at"`
}

type attachmentResponse struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	MIMEType     string `json:"mime_type,omitempty"`
	FileName     string `json:"file_name,omitempty"`
	Size         uint64 `json:"size,omitempty"`
	Availability string `json:"availability"`
}

func messageFromCore(m core.Message) messageResponse {
	var content any = map[string]string{}
	if m.Kind == core.MessageKindText {
		content = map[string]string{"text": m.Text}
	} else if m.Text != "" {
		content = map[string]string{"caption": m.Text}
	}
	attachments := make([]attachmentResponse, 0, len(m.Attachments))
	for _, attachment := range m.Attachments {
		attachments = append(attachments, attachmentResponse{ID: attachment.ID, Kind: string(attachment.Kind),
			MIMEType: attachment.MIMEType, FileName: attachment.FileName, Size: attachment.Size, Availability: attachment.Availability})
	}
	return messageResponse{ID: m.ID, AccountID: m.AccountID, ConversationID: m.ConversationID,
		ProviderMessageID: m.ProviderMessageID, Direction: m.Direction, State: m.State, SenderID: m.SenderID,
		Kind: string(m.Kind), Content: content, Attachments: attachments, OccurredAt: m.OccurredAt, IngestedAt: m.IngestedAt}
}

func attachmentFromRecord(record core.MediaRecord) attachmentResponse {
	size := record.DeclaredSize
	if record.Availability == "ready" {
		size = uint64(record.StoredSize)
	}
	return attachmentResponse{ID: record.AttachmentID, Kind: string(record.Kind), MIMEType: record.MIMEType,
		FileName: record.FileName, Size: size, Availability: record.Availability}
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

func conversationFromCore(c core.Conversation) conversationResponse {
	result := conversationResponse{ID: c.ID, AccountID: c.AccountID, ProviderChatID: c.ProviderChatID,
		Kind: c.Kind, DisplayName: c.DisplayName, Description: c.Description, AvatarURL: "/api/v1/conversations/" + c.ID + "/avatar",
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.Contact != nil {
		contact := contactFromCore(c.AccountID, *c.Contact)
		result.Contact = &contact
	}
	if c.LastMessage != nil {
		last := messageFromCore(*c.LastMessage)
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
