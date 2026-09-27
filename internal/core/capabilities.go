package core

import (
	"errors"
	"time"
)

var ErrUnsupported = errors.New("operation is not supported")

type MessageActions struct {
	Reply       bool       `json:"reply"`
	React       bool       `json:"react"`
	Edit        bool       `json:"edit"`
	Revoke      bool       `json:"revoke"`
	Receipts    bool       `json:"receipts"`
	EditUntil   *time.Time `json:"edit_until,omitempty"`
	RevokeUntil *time.Time `json:"revoke_until,omitempty"`
}

type CapabilityProvider interface {
	Capabilities() []string
	MessageActions(Message, time.Time) MessageActions
}
