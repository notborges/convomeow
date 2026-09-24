package native_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/core"
	"github.com/notborges/convomeow/internal/media"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func TestAttachmentDownloadAndRanges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := sqlite.Open(ctx, filepath.Join(dir, "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := uuid.NewString()
	now := time.Now().UTC()
	if err := repo.CreateAccount(ctx, core.Account{ID: accountID, Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "media",
		CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetIdentity(ctx, accountID, "test-device"); err != nil {
		t.Fatal(err)
	}
	message, err := repo.SaveMessage(ctx, core.Message{AccountID: accountID, ChatID: "15551112222@s.whatsapp.net",
		ProviderMessageID: "media-1", Direction: "inbound", Kind: core.MessageKindImage, OccurredAt: now,
		Attachments: []core.Attachment{{Kind: core.MessageKindImage, MIMEType: "image/jpeg", FileName: "photo.jpg",
			Availability: "remote", ProviderRef: []byte("private-ref")}}})
	if err != nil {
		t.Fatal(err)
	}
	id := message.Attachments[0].ID
	local, err := media.NewLocal(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := media.NewRegistry("local", map[string]media.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	connector := &fakeConnector{mediaPayload: []byte("0123456789"), mediaGate: gate}
	service := app.NewWithMedia(repo, connector, nil, app.MediaOptions{Stores: registry, TempDir: filepath.Join(dir, "tmp"),
		MaxFileBytes: 32, MaxTotalBytes: 1024, MaxTempBytes: (1 << 20) + 32, Workers: 1})
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	waitFor(t, func() bool {
		status, err := service.Account(accountID)
		return err == nil && status.State == "connected"
	})
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	path := server.URL + "/api/v1/attachments/" + id
	content := path + "/content"

	unauthorized, err := server.Client().Get(content)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized content: %d", unauthorized.StatusCode)
	}
	metadata := decode[struct {
		Availability string `json:"availability"`
	}](t, request(t, server.Client(), http.MethodGet, path, nil, ""), http.StatusOK)
	if metadata.Availability != "remote" {
		t.Fatalf("initial availability: %s", metadata.Availability)
	}
	head := request(t, server.Client(), http.MethodHead, content, nil, "")
	head.Body.Close()
	if head.StatusCode != http.StatusAccepted || connector.mediaDownloads.Load() != 0 {
		t.Fatalf("remote HEAD: status=%d downloads=%d", head.StatusCode, connector.mediaDownloads.Load())
	}
	first := request(t, server.Client(), http.MethodGet, content, nil, "")
	firstData, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusAccepted || first.Header.Get("Retry-After") == "" || strings.Contains(string(firstData), "private-ref") {
		t.Fatalf("remote GET: status=%d body=%s", first.StatusCode, firstData)
	}
	waitFor(t, func() bool { return connector.mediaDownloads.Load() == 1 })
	second := request(t, server.Client(), http.MethodGet, content, nil, "")
	second.Body.Close()
	if second.StatusCode != http.StatusAccepted || connector.mediaDownloads.Load() != 1 {
		t.Fatalf("duplicate fetch: status=%d downloads=%d", second.StatusCode, connector.mediaDownloads.Load())
	}
	close(gate)
	waitFor(t, func() bool {
		record, err := repo.GetMedia(ctx, id)
		return err == nil && record.Availability == "ready"
	})
	full := request(t, server.Client(), http.MethodGet, content, nil, "")
	fullData, _ := io.ReadAll(full.Body)
	full.Body.Close()
	if full.StatusCode != http.StatusOK || string(fullData) != "0123456789" || full.Header.Get("Content-Type") != "image/jpeg" ||
		full.Header.Get("Cache-Control") != "private, no-store" || full.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("ready content: status=%d headers=%v body=%q", full.StatusCode, full.Header, fullData)
	}
	rangeReq, _ := http.NewRequest(http.MethodGet, content, nil)
	rangeReq.Header.Set("Authorization", "Bearer test-token")
	rangeReq.Header.Set("Range", "bytes=2-5")
	ranged, err := server.Client().Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	rangeData, _ := io.ReadAll(ranged.Body)
	ranged.Body.Close()
	if ranged.StatusCode != http.StatusPartialContent || string(rangeData) != "2345" || ranged.Header.Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range: status=%d headers=%v body=%q", ranged.StatusCode, ranged.Header, rangeData)
	}
	rangeReq, _ = http.NewRequest(http.MethodGet, content, nil)
	rangeReq.Header.Set("Authorization", "Bearer test-token")
	rangeReq.Header.Set("Range", "bytes=99-")
	invalidRange, err := server.Client().Do(rangeReq)
	if err != nil {
		t.Fatal(err)
	}
	invalidRange.Body.Close()
	if invalidRange.StatusCode != http.StatusRequestedRangeNotSatisfiable || invalidRange.Header.Get("Content-Range") != "bytes */10" {
		t.Fatalf("invalid range: status=%d headers=%v", invalidRange.StatusCode, invalidRange.Header)
	}
	head = request(t, server.Client(), http.MethodHead, content, nil, "")
	head.Body.Close()
	if head.StatusCode != http.StatusOK || head.Header.Get("Content-Length") != "10" {
		t.Fatalf("ready HEAD: status=%d headers=%v", head.StatusCode, head.Header)
	}
	key, _ := media.Key(accountID, id)
	if err := local.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	missing := request(t, server.Client(), http.MethodGet, content, nil, "")
	missing.Body.Close()
	if missing.StatusCode != http.StatusAccepted {
		t.Fatalf("missing object: %d", missing.StatusCode)
	}
	waitFor(t, func() bool { return connector.mediaDownloads.Load() == 2 })
	waitFor(t, func() bool {
		record, err := repo.GetMedia(ctx, id)
		return err == nil && record.Availability == "ready"
	})
	_, err = os.Stat(filepath.Join(dir, "media", filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}
}

func TestPendingMediaRecoveryAndLimits(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := sqlite.Open(ctx, filepath.Join(dir, "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := uuid.NewString()
	now := time.Now().UTC()
	if err := repo.CreateAccount(ctx, core.Account{ID: accountID, Provider: core.ProviderWhatsApp,
		ConnectionKind: core.ConnectionKindLinkedDevice, Label: "media", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetIdentity(ctx, accountID, "test-device"); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 3)
	for i, declaredSize := range []uint64{10, 10, 100} {
		message, err := repo.SaveMessage(ctx, core.Message{AccountID: accountID, ChatID: "15551112222@s.whatsapp.net",
			ProviderMessageID: "pending-" + string(rune('a'+i)), Direction: "inbound", Kind: core.MessageKindImage,
			OccurredAt: now.Add(time.Duration(i) * time.Second), Attachments: []core.Attachment{{Kind: core.MessageKindImage,
				MIMEType: "image/jpeg", Size: declaredSize, Availability: "remote", ProviderRef: []byte("private-ref"), AutoFetch: true}}})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = message.Attachments[0].ID
	}
	local, err := media.NewLocal(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := media.NewRegistry("local", map[string]media.Store{"local": local})
	connector := &fakeConnector{mediaPayload: []byte("0123456789")}
	service := app.NewWithMedia(repo, connector, nil, app.MediaOptions{Stores: registry, TempDir: filepath.Join(dir, "tmp"),
		MaxFileBytes: 16, MaxTotalBytes: 16, MaxTempBytes: (1 << 20) + 16, Workers: 1})
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = service.Close() }()
	waitFor(t, func() bool {
		ready, quota, large := 0, 0, 0
		for _, id := range ids {
			record, err := repo.GetMedia(ctx, id)
			if err != nil {
				return false
			}
			switch {
			case record.Availability == "ready":
				ready++
			case record.FailureCode == "quota":
				quota++
			case record.FailureCode == "too_large":
				large++
			}
		}
		return ready == 1 && quota == 1 && large == 1
	})
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	var readyID, quotaID, largeID string
	for _, id := range ids {
		record, err := repo.GetMedia(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		want := http.StatusOK
		if record.FailureCode == "quota" {
			quotaID = id
			want = http.StatusInsufficientStorage
		} else if record.FailureCode == "too_large" {
			largeID = id
			want = http.StatusRequestEntityTooLarge
		} else {
			readyID = id
		}
		response := request(t, server.Client(), http.MethodGet, server.URL+"/api/v1/attachments/"+id+"/content", nil, "")
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("media %s failure=%s: got %d, want %d", id, record.FailureCode, response.StatusCode, want)
		}
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, filepath.Join(dir, "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	newLocal, err := media.NewLocal(filepath.Join(dir, "other-media"))
	if err != nil {
		t.Fatal(err)
	}
	switched, err := media.NewRegistry("other", map[string]media.Store{"local": local, "other": newLocal})
	if err != nil {
		t.Fatal(err)
	}
	service = app.NewWithMedia(reopened, connector, nil, app.MediaOptions{Stores: switched, TempDir: filepath.Join(dir, "tmp"),
		MaxFileBytes: 128, MaxTotalBytes: 128, MaxTempBytes: (1 << 20) + 128, Workers: 1})
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		status, err := service.Account(accountID)
		return err == nil && status.State == "connected"
	})
	switchedServer := httptest.NewServer(native.New(service, "test-token"))
	defer switchedServer.Close()
	oldFile := request(t, switchedServer.Client(), http.MethodGet,
		switchedServer.URL+"/api/v1/attachments/"+readyID+"/content", nil, "")
	oldData, _ := io.ReadAll(oldFile.Body)
	oldFile.Body.Close()
	if oldFile.StatusCode != http.StatusOK || string(oldData) != "0123456789" {
		t.Fatalf("old profile content: status=%d body=%q", oldFile.StatusCode, oldData)
	}
	for _, id := range []string{quotaID, largeID} {
		response := request(t, switchedServer.Client(), http.MethodGet,
			switchedServer.URL+"/api/v1/attachments/"+id+"/content", nil, "")
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("unblocked download %s: %d", id, response.StatusCode)
		}
	}
	waitFor(t, func() bool {
		for _, id := range []string{quotaID, largeID} {
			record, err := reopened.GetMedia(ctx, id)
			if err != nil || record.Availability != "ready" || record.StorageProfileID != "other" {
				return false
			}
		}
		return true
	})
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not reached")
}
