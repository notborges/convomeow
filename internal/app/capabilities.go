package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func (s *Service) capabilities() []string {
	if provider, ok := s.connector.(core.CapabilityProvider); ok {
		return provider.Capabilities()
	}
	return []string{}
}
func (s *Service) requireCapability(name string) error {
	if !slices.Contains(s.capabilities(), name) {
		return fmt.Errorf("%w: %s", core.ErrUnsupported, name)
	}
	return nil
}
func (s *Service) MessageActions(m core.Message) core.MessageActions {
	if provider, ok := s.connector.(core.CapabilityProvider); ok {
		a := provider.MessageActions(m, time.Now().UTC())
		caps := provider.Capabilities()
		a.Reply = a.Reply && slices.Contains(caps, "replies")
		a.React = a.React && slices.Contains(caps, "reactions")
		a.Edit = a.Edit && slices.Contains(caps, "edit_messages")
		a.Revoke = a.Revoke && slices.Contains(caps, "revoke_messages")
		a.Receipts = a.Receipts && slices.Contains(caps, "message_receipts")
		return a
	}
	return core.MessageActions{}
}
func (s *Service) accountStatus(rt *runtimeAccount) core.AccountStatus {
	status := rt.status()
	status.Capabilities = s.capabilities()
	return status
}
