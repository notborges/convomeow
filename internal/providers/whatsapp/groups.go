package whatsapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func (s *session) CreateGroup(ctx context.Context, name string, participants []string) (core.Group, error) {
	jids, err := participantJIDs(participants)
	if err != nil {
		return core.Group{}, err
	}
	info, err := s.client.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: jids})
	if err != nil {
		return core.Group{}, groupError(err)
	}
	return s.groupFromInfo(info), nil
}

func (s *session) Group(ctx context.Context, chatID string) (core.Group, error) {
	jid, err := groupJID(chatID)
	if err != nil {
		return core.Group{}, err
	}
	info, err := s.client.GetGroupInfo(ctx, jid)
	if err != nil {
		return core.Group{}, groupError(err)
	}
	return s.groupFromInfo(info), nil
}

func (s *session) UpdateGroup(ctx context.Context, chatID string, update core.GroupUpdate) error {
	jid, err := groupJID(chatID)
	if err != nil {
		return err
	}
	if update.Name != nil {
		if err := s.client.SetGroupName(ctx, jid, *update.Name); err != nil {
			return groupError(err)
		}
	}
	if update.Description != nil {
		if err := s.client.SetGroupTopic(ctx, jid, "", "", *update.Description); err != nil {
			return groupError(err)
		}
	}
	if update.AdminsOnlySend != nil {
		if err := s.client.SetGroupAnnounce(ctx, jid, *update.AdminsOnlySend); err != nil {
			return groupError(err)
		}
	}
	if update.AdminsOnlyEdit != nil {
		if err := s.client.SetGroupLocked(ctx, jid, *update.AdminsOnlyEdit); err != nil {
			return groupError(err)
		}
	}
	return nil
}

func (s *session) ChangeGroupParticipants(ctx context.Context, chatID string, action core.GroupParticipantAction, participants []string) ([]core.GroupParticipantResult, error) {
	jid, err := groupJID(chatID)
	if err != nil {
		return nil, err
	}
	var change whatsmeow.ParticipantChange
	switch action {
	case core.GroupParticipantAdd:
		change = whatsmeow.ParticipantChangeAdd
	case core.GroupParticipantRemove:
		change = whatsmeow.ParticipantChangeRemove
	case core.GroupParticipantPromote:
		change = whatsmeow.ParticipantChangePromote
	case core.GroupParticipantDemote:
		change = whatsmeow.ParticipantChangeDemote
	default:
		return nil, fmt.Errorf("%w: unknown participant action", core.ErrInvalid)
	}
	requested, err := participantJIDs(participants)
	if err != nil {
		return nil, err
	}
	var results []core.GroupParticipantResult
	targets := requested
	if action != core.GroupParticipantAdd {
		// LID-addressed groups only accept the member's address as listed in the group,
		// so map phone numbers and alternate IDs to that address first.
		info, err := s.client.GetGroupInfo(ctx, jid)
		if err != nil {
			return nil, groupError(err)
		}
		members := make(map[types.JID]types.JID)
		for _, p := range info.Participants {
			for _, alias := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
				if !alias.IsEmpty() {
					members[alias.ToNonAD()] = p.JID
				}
			}
		}
		targets = targets[:0:0]
		for _, requestedJID := range requested {
			if member, ok := members[requestedJID]; ok {
				targets = append(targets, member)
			} else {
				results = append(results, core.GroupParticipantResult{ProviderID: requestedJID.String(), Status: "not_participant"})
			}
		}
	}
	if len(targets) == 0 {
		return results, nil
	}
	changed, err := s.client.UpdateGroupParticipants(ctx, jid, targets, change)
	if err != nil {
		return nil, groupError(err)
	}
	for _, p := range changed {
		results = append(results, core.GroupParticipantResult{ProviderID: participantID(p).String(), Status: participantStatus(p.Error)})
	}
	return results, nil
}

func (s *session) LeaveGroup(ctx context.Context, chatID string) error {
	jid, err := groupJID(chatID)
	if err != nil {
		return err
	}
	return groupError(s.client.LeaveGroup(ctx, jid))
}

func (s *session) GroupInviteLink(ctx context.Context, chatID string, reset bool) (string, error) {
	jid, err := groupJID(chatID)
	if err != nil {
		return "", err
	}
	link, err := s.client.GetGroupInviteLink(ctx, jid, reset)
	if err != nil {
		return "", groupError(err)
	}
	return link, nil
}

func (s *session) groupFromInfo(info *types.GroupInfo) core.Group {
	self := map[types.JID]bool{}
	if s.client.Store != nil {
		for _, jid := range []types.JID{s.client.Store.GetJID(), s.client.Store.GetLID()} {
			if !jid.IsEmpty() {
				self[jid.ToNonAD()] = true
			}
		}
	}
	group := core.Group{ProviderChatID: info.JID.ToNonAD().String(), Name: info.Name, Description: info.Topic,
		AdminsOnlySend: info.IsAnnounce, AdminsOnlyEdit: info.IsLocked, OwnRole: core.GroupRoleMember,
		Participants: make([]core.GroupParticipant, 0, len(info.Participants))}
	if !info.GroupCreated.IsZero() {
		created := info.GroupCreated.UTC()
		group.CreatedAt = &created
	}
	for _, p := range info.Participants {
		id := participantID(p)
		if p.Error != 0 {
			group.FailedParticipants = append(group.FailedParticipants, core.GroupParticipantResult{ProviderID: id.String(), Status: participantStatus(p.Error)})
			continue
		}
		participant := core.GroupParticipant{ProviderID: id.String(), Role: core.GroupRoleMember,
			IsSelf: self[p.JID.ToNonAD()] || self[p.PhoneNumber.ToNonAD()] || self[p.LID.ToNonAD()]}
		if id.Server == types.DefaultUserServer {
			participant.Phone = "+" + id.User
		}
		if !p.LID.IsEmpty() && p.LID.ToNonAD() != id {
			participant.AlternateID = p.LID.ToNonAD().String()
		}
		if p.IsSuperAdmin {
			participant.Role = core.GroupRoleOwner
		} else if p.IsAdmin {
			participant.Role = core.GroupRoleAdmin
		}
		if participant.IsSelf {
			group.OwnRole = participant.Role
		}
		group.Participants = append(group.Participants, participant)
	}
	return group
}

// participantID prefers the phone-number JID, matching the IDs used for contacts.
func participantID(p types.GroupParticipant) types.JID {
	if !p.PhoneNumber.IsEmpty() {
		return p.PhoneNumber.ToNonAD()
	}
	return p.JID.ToNonAD()
}

func participantStatus(code int) string {
	switch code {
	case 0:
		return "ok"
	case 401:
		return "forbidden"
	case 403:
		return "privacy_restricted"
	case 404:
		return "not_found"
	case 408:
		return "recently_left"
	case 409:
		return "already_participant"
	default:
		return "rejected"
	}
}

func groupJID(chatID string) (types.JID, error) {
	jid, err := types.ParseJID(chatID)
	if err != nil || jid.IsEmpty() || jid.Server != types.GroupServer {
		return types.JID{}, fmt.Errorf("%w: conversation is not a group", core.ErrInvalid)
	}
	return jid, nil
}

func participantJIDs(values []string) ([]types.JID, error) {
	seen := map[types.JID]bool{}
	jids := make([]types.JID, 0, len(values))
	for _, value := range values {
		jid, err := recipientJID(value)
		if err != nil {
			return nil, err
		}
		jid = jid.ToNonAD()
		if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
			return nil, fmt.Errorf("%w: participants must be phone numbers or user IDs", core.ErrInvalid)
		}
		if !seen[jid] {
			seen[jid] = true
			jids = append(jids, jid)
		}
	}
	return jids, nil
}

func groupError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, whatsmeow.ErrNotConnected), errors.Is(err, whatsmeow.ErrIQDisconnected):
		return fmt.Errorf("%w: %v", core.ErrNotConnected, err)
	case errors.Is(err, whatsmeow.ErrNotInGroup), errors.Is(err, whatsmeow.ErrIQForbidden):
		return fmt.Errorf("%w: %v", core.ErrNotParticipant, err)
	case errors.Is(err, whatsmeow.ErrGroupInviteLinkUnauthorized), errors.Is(err, whatsmeow.ErrIQNotAuthorized):
		return fmt.Errorf("%w: %v", core.ErrForbidden, err)
	case errors.Is(err, whatsmeow.ErrGroupNotFound), errors.Is(err, whatsmeow.ErrIQNotFound):
		return fmt.Errorf("%w: %v", core.ErrNotFound, err)
	case errors.Is(err, whatsmeow.ErrIQBadRequest), errors.Is(err, whatsmeow.ErrIQNotAcceptable):
		return fmt.Errorf("%w: WhatsApp rejected the group change", core.ErrInvalid)
	default:
		return err
	}
}
