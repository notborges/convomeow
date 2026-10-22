package whatsapp

import (
	"errors"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func TestGroupFromInfo(t *testing.T) {
	self := types.NewJID("100", types.DefaultUserServer)
	selfLID := types.NewJID("900", types.HiddenUserServer)
	device := &store.Device{ID: &types.JID{User: "100", Device: 3, Server: types.DefaultUserServer}, LID: selfLID}
	s := &session{client: whatsmeow.NewClient(device, nil)}
	created := time.Unix(1700000000, 0)
	info := &types.GroupInfo{JID: types.NewJID("team", types.GroupServer), GroupName: types.GroupName{Name: "Team"},
		GroupTopic: types.GroupTopic{Topic: "Plans"}, GroupAnnounce: types.GroupAnnounce{IsAnnounce: true}, GroupCreated: created,
		Participants: []types.GroupParticipant{
			{JID: selfLID, LID: selfLID, PhoneNumber: self, IsAdmin: true},
			{JID: types.NewJID("901", types.HiddenUserServer), LID: types.NewJID("901", types.HiddenUserServer), PhoneNumber: types.NewJID("200", types.DefaultUserServer), IsAdmin: true, IsSuperAdmin: true},
			{JID: types.NewJID("902", types.HiddenUserServer), LID: types.NewJID("902", types.HiddenUserServer)},
			{JID: types.NewJID("300", types.DefaultUserServer), Error: 403},
		}}
	group := s.groupFromInfo(info)
	if group.ProviderChatID != "team@g.us" || group.Name != "Team" || group.Description != "Plans" || !group.AdminsOnlySend || group.AdminsOnlyEdit {
		t.Fatalf("group fields: %+v", group)
	}
	if group.CreatedAt == nil || !group.CreatedAt.Equal(created) || group.OwnRole != core.GroupRoleAdmin {
		t.Fatalf("created or own role: %+v", group)
	}
	if len(group.Participants) != 3 {
		t.Fatalf("participants: %+v", group.Participants)
	}
	if p := group.Participants[0]; !p.IsSelf || p.ProviderID != "100@s.whatsapp.net" || p.Phone != "+100" || p.AlternateID != "900@lid" {
		t.Fatalf("self participant: %+v", p)
	}
	if p := group.Participants[1]; p.Role != core.GroupRoleOwner || p.ProviderID != "200@s.whatsapp.net" || p.IsSelf {
		t.Fatalf("owner participant: %+v", p)
	}
	if p := group.Participants[2]; p.Role != core.GroupRoleMember || p.ProviderID != "902@lid" || p.Phone != "" {
		t.Fatalf("hidden participant: %+v", p)
	}
	if len(group.FailedParticipants) != 1 || group.FailedParticipants[0] != (core.GroupParticipantResult{ProviderID: "300@s.whatsapp.net", Status: "privacy_restricted"}) {
		t.Fatalf("failed participants: %+v", group.FailedParticipants)
	}
}

func TestParticipantJIDs(t *testing.T) {
	jids, err := participantJIDs([]string{"+15551234567", "15551234567@s.whatsapp.net", "15551234567:2@s.whatsapp.net", "901@lid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(jids) != 2 || jids[0].String() != "15551234567@s.whatsapp.net" || jids[1].String() != "901@lid" {
		t.Fatalf("participants: %v", jids)
	}
	for _, value := range []string{"team@g.us", "abc", "123"} {
		if _, err := participantJIDs([]string{value}); !errors.Is(err, core.ErrInvalid) {
			t.Fatalf("%q accepted: %v", value, err)
		}
	}
	if _, err := groupJID("15551234567@s.whatsapp.net"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("direct chat accepted as group: %v", err)
	}
}

func TestGroupErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want error
	}{
		{whatsmeow.ErrNotInGroup, core.ErrNotParticipant},
		{whatsmeow.ErrIQForbidden, core.ErrNotParticipant},
		{whatsmeow.ErrIQNotAuthorized, core.ErrForbidden},
		{whatsmeow.ErrGroupInviteLinkUnauthorized, core.ErrForbidden},
		{whatsmeow.ErrGroupNotFound, core.ErrNotFound},
		{whatsmeow.ErrIQNotAcceptable, core.ErrInvalid},
		{whatsmeow.ErrNotConnected, core.ErrNotConnected},
	} {
		if got := groupError(tc.err); !errors.Is(got, tc.want) {
			t.Fatalf("groupError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
	if groupError(nil) != nil {
		t.Fatal("nil error changed")
	}
	statuses := map[int]string{0: "ok", 401: "forbidden", 403: "privacy_restricted", 404: "not_found", 408: "recently_left", 409: "already_participant", 500: "rejected"}
	for code, want := range statuses {
		if got := participantStatus(code); got != want {
			t.Fatalf("participantStatus(%d) = %q, want %q", code, got, want)
		}
	}
}
