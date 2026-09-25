package native_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func avatarTestService(t *testing.T, dbPath, mediaPath string, connector *fakeConnector) (*app.Service, *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	local, err := media.NewLocal(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := media.NewRegistry("local", map[string]media.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewWithMedia(store, connector, nil, app.MediaOptions{Stores: registry,
		TempDir: filepath.Join(filepath.Dir(dbPath), "tmp"), MaxFileBytes: 1 << 20,
		MaxTotalBytes: 10 << 20, MaxTempBytes: 2 << 20, Workers: 1})
	if err := service.Start(ctx); err != nil {
		service.Close()
		t.Fatal(err)
	}
	return service, store
}

func createAvatarTestAccount(t *testing.T, dbPath, accountID string) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err := store.CreateAccount(context.Background(), core.Account{ID: accountID, Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "sales", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetIdentity(context.Background(), accountID, "fake-device"); err != nil {
		t.Fatal(err)
	}
}

func waitForCachedAvatar(t *testing.T, store *sqlite.Store, accountID, providerID string) core.AvatarRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.GetAvatar(context.Background(), accountID, providerID)
		if err == nil && record.ObjectKey != "" {
			return record
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("avatar was not prefetched")
	return core.AvatarRecord{}
}

func TestAvatarCachePrefetchRefreshAndOfflineRead(t *testing.T) {
	root := t.TempDir()
	dbPath, mediaPath := filepath.Join(root, "app.sqlite"), filepath.Join(root, "media")
	accountID := uuid.NewString()
	providerID := "111@s.whatsapp.net"
	createAvatarTestAccount(t, dbPath, accountID)
	photo := []byte("photo-data")
	connector := &fakeConnector{contacts: map[string]core.Contact{providerID: {ProviderID: providerID, Name: "Alice"}},
		avatar: core.Avatar{ContentType: "image/png", Data: photo, PictureID: "picture-1"}}
	service, store := avatarTestService(t, dbPath, mediaPath, connector)
	record := waitForCachedAvatar(t, store, accountID, providerID)
	if connector.avatarFetches.Load() != 1 {
		t.Fatalf("prefetch count = %d", connector.avatarFetches.Load())
	}
	if err := store.TouchAvatar(context.Background(), accountID, providerID, time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	avatar, err := service.Avatar(context.Background(), accountID, providerID)
	if err != nil || !bytes.Equal(avatar.Data, photo) {
		t.Fatalf("refresh cached avatar: %+v, %v", avatar, err)
	}
	refreshed, err := store.GetAvatar(context.Background(), accountID, providerID)
	if err != nil || refreshed.ObjectKey != record.ObjectKey || connector.avatarFetches.Load() != 2 {
		t.Fatalf("conditional refresh: %+v, fetches=%d, error=%v", refreshed, connector.avatarFetches.Load(), err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	offline := &fakeConnector{offline: true, contacts: connector.contacts}
	service, offlineStore := avatarTestService(t, dbPath, mediaPath, offline)
	defer service.Close()
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	response := request(t, server.Client(), http.MethodGet,
		server.URL+"/api/v1/accounts/"+accountID+"/contacts/"+providerID+"/avatar", nil, "")
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(data, photo) || offline.avatarFetches.Load() != 0 {
		t.Fatalf("offline cached avatar: status=%d data=%q fetches=%d error=%v", response.StatusCode, data, offline.avatarFetches.Load(), err)
	}
	offline.emitEvent(core.Event{Type: core.EventLoggedOut})
	if _, err := offlineStore.GetAvatar(context.Background(), accountID, providerID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("logout did not clear cached photo: %v", err)
	}
	if _, err := service.Avatar(context.Background(), accountID, providerID); !errors.Is(err, core.ErrNotConnected) {
		t.Fatalf("logged-out account served a photo: %v", err)
	}
	offline.eventMu.Lock()
	oldSessionEvent := offline.event
	offline.eventMu.Unlock()
	if _, err := service.StartLogin(accountID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		account, err := service.Account(accountID)
		if err == nil && account.State == "connected" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	oldSessionEvent(core.Event{Type: core.EventLoggedOut})
	account, err := service.Account(accountID)
	if err != nil || account.State != "connected" || account.ProviderIdentity == "" {
		t.Fatalf("old session changed new account: %+v, %v", account, err)
	}
}

func TestAvatarCacheDropsPhotoWhenProviderHidesIt(t *testing.T) {
	root := t.TempDir()
	dbPath, mediaPath := filepath.Join(root, "app.sqlite"), filepath.Join(root, "media")
	accountID := uuid.NewString()
	providerID := "111@s.whatsapp.net"
	createAvatarTestAccount(t, dbPath, accountID)
	connector := &fakeConnector{contacts: map[string]core.Contact{providerID: {ProviderID: providerID}},
		avatar: core.Avatar{ContentType: "image/png", Data: []byte("photo"), PictureID: "picture-1"}}
	service, store := avatarTestService(t, dbPath, mediaPath, connector)
	defer service.Close()
	waitForCachedAvatar(t, store, accountID, providerID)
	connector.setAvatar(core.Avatar{})
	if err := store.TouchAvatar(context.Background(), accountID, providerID, time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Avatar(context.Background(), accountID, providerID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("hidden avatar: %v", err)
	}
	missing, err := store.GetAvatar(context.Background(), accountID, providerID)
	if err != nil || missing.ObjectKey != "" {
		t.Fatalf("old avatar remained cached: %+v, %v", missing, err)
	}
	if _, err := service.Avatar(context.Background(), accountID, providerID); !errors.Is(err, core.ErrNotFound) || connector.avatarFetches.Load() != 2 {
		t.Fatalf("missing avatar was fetched again: fetches=%d error=%v", connector.avatarFetches.Load(), err)
	}
}

func TestAvatarChangeEventsRefreshAndRemove(t *testing.T) {
	root := t.TempDir()
	dbPath, mediaPath := filepath.Join(root, "app.sqlite"), filepath.Join(root, "media")
	accountID := uuid.NewString()
	providerID := "111@s.whatsapp.net"
	createAvatarTestAccount(t, dbPath, accountID)
	connector := &fakeConnector{contacts: map[string]core.Contact{providerID: {ProviderID: providerID}},
		avatar: core.Avatar{ContentType: "image/png", Data: []byte("first"), PictureID: "picture-1"}}
	service, store := avatarTestService(t, dbPath, mediaPath, connector)
	defer service.Close()
	first := waitForCachedAvatar(t, store, accountID, providerID)
	connector.setAvatar(core.Avatar{ContentType: "image/png", Data: []byte("second"), PictureID: "picture-2"})
	connector.emitEvent(core.Event{Type: core.EventAvatarChanged, AvatarID: providerID})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.GetAvatar(context.Background(), accountID, providerID)
		if err == nil && record.PictureID == "picture-2" && record.ObjectKey != first.ObjectKey {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	refreshed, err := store.GetAvatar(context.Background(), accountID, providerID)
	if err != nil || refreshed.PictureID != "picture-2" || refreshed.ObjectKey == first.ObjectKey {
		t.Fatalf("change event did not refresh avatar: %+v, %v", refreshed, err)
	}
	connector.emitEvent(core.Event{Type: core.EventAvatarChanged, AvatarID: providerID, AvatarRemoved: true})
	if _, err := service.Avatar(context.Background(), accountID, providerID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("removed avatar remained available: %v", err)
	}
}

func TestAvatarRemovalDuringRefreshDoesNotServeOldPhoto(t *testing.T) {
	root := t.TempDir()
	dbPath, mediaPath := filepath.Join(root, "app.sqlite"), filepath.Join(root, "media")
	accountID := uuid.NewString()
	providerID := "111@s.whatsapp.net"
	createAvatarTestAccount(t, dbPath, accountID)
	connector := &fakeConnector{contacts: map[string]core.Contact{providerID: {ProviderID: providerID}},
		avatar: core.Avatar{ContentType: "image/png", Data: []byte("old-photo"), PictureID: "picture-1"}}
	service, store := avatarTestService(t, dbPath, mediaPath, connector)
	defer service.Close()
	waitForCachedAvatar(t, store, accountID, providerID)
	if err := store.TouchAvatar(context.Background(), accountID, providerID, time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	connector.setAvatarGate(gate, started)
	result := make(chan error, 1)
	go func() {
		_, err := service.Avatar(context.Background(), accountID, providerID)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(gate)
		t.Fatal("refresh did not start")
	}
	connector.emitEvent(core.Event{Type: core.EventAvatarChanged, AvatarID: providerID, AvatarRemoved: true})
	close(gate)
	select {
	case err := <-result:
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("removed photo returned from stale cache: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
}
