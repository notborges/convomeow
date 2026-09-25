package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const maxAvatarBytes = 512 << 10

func (s *session) Contact(ctx context.Context, providerID string) (core.Contact, error) {
	jid, err := contactJID(providerID)
	if err != nil {
		return core.Contact{}, err
	}
	info, err := s.client.Store.Contacts.GetContact(ctx, jid)
	if err != nil {
		return core.Contact{}, err
	}
	alternate := ""
	if jid.Server == types.HiddenUserServer {
		pn, err := s.client.Store.LIDs.GetPNForLID(ctx, jid)
		if err != nil {
			return core.Contact{}, err
		}
		if !pn.IsEmpty() {
			alternate = jid.String()
			pnInfo, err := s.client.Store.Contacts.GetContact(ctx, pn)
			if err != nil {
				return core.Contact{}, err
			}
			info = mergeContactInfo(pnInfo, info)
			jid = pn
		}
	} else {
		lid, err := s.client.Store.LIDs.GetLIDForPN(ctx, jid)
		if err != nil {
			return core.Contact{}, err
		}
		if !lid.IsEmpty() {
			alternate = lid.String()
			lidInfo, err := s.client.Store.Contacts.GetContact(ctx, lid)
			if err != nil {
				return core.Contact{}, err
			}
			info = mergeContactInfo(info, lidInfo)
		}
	}
	if !info.Found {
		return core.Contact{}, core.ErrNotFound
	}
	contact := contactFromInfo(jid, info)
	contact.AlternateID = alternate
	return contact, nil
}

func (s *session) Contacts(ctx context.Context) ([]core.Contact, error) {
	entries, err := s.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	merged := make(map[types.JID]types.ContactInfo, len(entries))
	alternates := make(map[types.JID]string)
	for jid, info := range entries {
		if jid.Server == types.DefaultUserServer {
			merged[jid.ToNonAD()] = info
		}
	}
	for jid, info := range entries {
		if jid.Server != types.HiddenUserServer {
			continue
		}
		jid = jid.ToNonAD()
		pn, err := s.client.Store.LIDs.GetPNForLID(ctx, jid)
		if err != nil {
			return nil, err
		}
		if !pn.IsEmpty() {
			alternates[pn] = jid.String()
			jid = pn
		}
		merged[jid] = mergeContactInfo(merged[jid], info)
	}
	contacts := make([]core.Contact, 0, len(merged))
	for jid, info := range merged {
		if info.Found {
			contact := contactFromInfo(jid, info)
			contact.AlternateID = alternates[jid]
			contacts = append(contacts, contact)
		}
	}
	sort.Slice(contacts, func(i, j int) bool { return contacts[i].ProviderID < contacts[j].ProviderID })
	return contacts, nil
}

func mergeContactInfo(primary, fallback types.ContactInfo) types.ContactInfo {
	primary.Found = primary.Found || fallback.Found
	if primary.FullName == "" {
		primary.FullName = fallback.FullName
	}
	if primary.FirstName == "" {
		primary.FirstName = fallback.FirstName
	}
	if primary.PushName == "" {
		primary.PushName = fallback.PushName
	}
	if primary.BusinessName == "" {
		primary.BusinessName = fallback.BusinessName
	}
	if primary.RedactedPhone == "" {
		primary.RedactedPhone = fallback.RedactedPhone
	}
	return primary
}

func contactJID(value string) (types.JID, error) {
	jid, err := types.ParseJID(value)
	if err != nil || jid.IsEmpty() || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
		return types.JID{}, core.ErrInvalid
	}
	return jid.ToNonAD(), nil
}

func contactFromInfo(jid types.JID, info types.ContactInfo) core.Contact {
	phone := ""
	if jid.Server == types.DefaultUserServer {
		phone = "+" + jid.User
	}
	name := info.FullName
	if name == "" {
		name = info.BusinessName
	}
	if name == "" {
		name = info.PushName
	}
	if name == "" {
		name = info.FirstName
	}
	if name == "" {
		name = phone
	}
	if name == "" {
		name = info.RedactedPhone
	}
	if name == "" {
		name = jid.String()
	}
	return core.Contact{ProviderID: jid.String(), Name: name, Phone: phone, MaskedPhone: info.RedactedPhone}
}

func (s *session) handleProfileEvent(evt any) {
	switch e := evt.(type) {
	case *events.Message:
		if e.Info.Chat.Server == types.GroupServer {
			s.emit(core.Event{Type: core.EventChatProfile, Profile: &core.ChatProfile{ProviderChatID: e.Info.Chat.ToNonAD().String(), Kind: "group"}})
		} else {
			s.emitContactProfile(e.Info.Chat)
		}
	case *events.Contact:
		s.emitContactProfile(e.JID)
	case *events.PushName:
		s.emitContactProfile(e.JID)
		if !e.JIDAlt.IsEmpty() {
			s.emitContactProfile(e.JIDAlt)
		}
	case *events.BusinessName:
		s.emitContactProfile(e.JID)
	case *events.GroupInfo:
		profile := core.ChatProfile{ProviderChatID: e.JID.ToNonAD().String(), Kind: "group"}
		if e.Name != nil {
			profile.DisplayName = &e.Name.Name
		}
		if e.Topic != nil {
			profile.Description = &e.Topic.Topic
		}
		s.emit(core.Event{Type: core.EventChatProfile, Profile: &profile})
	case *events.JoinedGroup:
		profile := core.ChatProfile{ProviderChatID: e.JID.ToNonAD().String(), Kind: "group", DisplayName: &e.Name, Description: &e.Topic, CreateConversation: true}
		s.emit(core.Event{Type: core.EventChatProfile, Profile: &profile})
	case *events.Picture:
		providerID := e.JID.ToNonAD().String()
		if e.JID.Server == types.DefaultUserServer || e.JID.Server == types.HiddenUserServer {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			contact, err := s.Contact(ctx, providerID)
			cancel()
			if err == nil {
				providerID = contact.ProviderID
			}
		}
		s.emit(core.Event{Type: core.EventAvatarChanged, AvatarID: providerID, AvatarRemoved: e.Remove})
	}
}

func (s *session) emitContactProfile(jid types.JID) {
	if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	contact, err := s.Contact(ctx, jid.ToNonAD().String())
	if err != nil {
		if !errors.Is(err, core.ErrNotFound) {
			s.client.Log.Warnf("Read contact %s: %v", jid, err)
			return
		}
		contact = contactFromInfo(jid.ToNonAD(), types.ContactInfo{})
	}
	name := contact.Name
	s.emit(core.Event{Type: core.EventChatProfile, Profile: &core.ChatProfile{ProviderChatID: contact.ProviderID,
		AlternateID: contact.AlternateID, Kind: "direct", DisplayName: &name}})
}

func (s *session) FetchAvatar(ctx context.Context, providerID, existingID string) (core.Avatar, bool, error) {
	jid, err := types.ParseJID(providerID)
	if err != nil || jid.IsEmpty() || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer && jid.Server != types.GroupServer) {
		return core.Avatar{}, false, core.ErrInvalid
	}
	params := &whatsmeow.GetProfilePictureParams{Preview: true, ExistingID: existingID}
	info, err := s.client.GetProfilePictureInfo(ctx, jid.ToNonAD(), params)
	if errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) && jid.Server == types.GroupServer {
		group, groupErr := s.client.GetGroupInfo(ctx, jid.ToNonAD())
		if groupErr == nil && group != nil && group.IsParent {
			params.IsCommunity = true
			info, err = s.client.GetProfilePictureInfo(ctx, jid.ToNonAD(), params)
		}
	}
	if errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) || errors.Is(err, whatsmeow.ErrProfilePictureNotSet) {
		return core.Avatar{}, false, core.ErrNotFound
	}
	if err != nil {
		return core.Avatar{}, false, fmt.Errorf("%w: %v", core.ErrAvatarFetch, err)
	}
	if info == nil {
		if existingID != "" {
			return core.Avatar{}, true, nil
		}
		return core.Avatar{}, false, core.ErrAvatarFetch
	}
	if info.URL == "" {
		return core.Avatar{}, false, core.ErrAvatarFetch
	}
	avatar, err := downloadAvatar(ctx, info.URL)
	if err != nil {
		return core.Avatar{}, false, fmt.Errorf("%w: %v", core.ErrAvatarFetch, err)
	}
	avatar.PictureID = info.ID
	return avatar, false, nil
}

func downloadAvatar(ctx context.Context, rawURL string) (core.Avatar, error) {
	if err := checkAvatarURL(rawURL); err != nil {
		return core.Avatar{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many avatar redirects")
		}
		return checkAvatarURL(req.URL.String())
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return core.Avatar{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return core.Avatar{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return core.Avatar{}, fmt.Errorf("avatar download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAvatarBytes+1))
	if err != nil {
		return core.Avatar{}, err
	}
	if len(data) == 0 || len(data) > maxAvatarBytes {
		return core.Avatar{}, core.ErrMediaUnavailable
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return core.Avatar{}, core.ErrMediaUnavailable
	}
	return core.Avatar{ContentType: contentType, Data: data}, nil
}

func checkAvatarURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	allowed := host == "whatsapp.net" || strings.HasSuffix(host, ".whatsapp.net") || host == "fbcdn.net" || strings.HasSuffix(host, ".fbcdn.net")
	if u.Scheme != "https" || u.User != nil || !allowed || (u.Port() != "" && u.Port() != "443") {
		return core.ErrMediaUnavailable
	}
	return nil
}
