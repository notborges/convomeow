package native_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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

func TestOutgoingMediaUploadSendAndUncertainRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.sqlite")
	repo, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	accounts := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range accounts {
		now := time.Now().UTC()
		if err := repo.CreateAccount(ctx, core.Account{ID: id, Provider: core.ProviderWhatsApp,
			ConnectionKind: core.ConnectionKindLinkedDevice, Label: string(rune('a' + i)), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := repo.SetIdentity(ctx, id, "device-"+id); err != nil {
			t.Fatal(err)
		}
	}
	local, err := media.NewLocal(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := media.NewRegistry("local", map[string]media.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	connector := &fakeConnector{}
	options := app.MediaOptions{Stores: registry, TempDir: filepath.Join(dir, "tmp"), MaxFileBytes: 4096,
		MaxTotalBytes: 8192, MaxTempBytes: (1 << 20) + 4096, Workers: 1}
	service := app.NewWithMedia(repo, connector, nil, options)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range accounts {
		waitFor(t, func() bool { status, err := service.Account(id); return err == nil && status.State == "connected" })
	}
	chatA, _, err := service.CreateConversation(ctx, accounts[0], core.ConversationTarget{Type: "phone_number", Value: "15551112222"})
	if err != nil {
		t.Fatal(err)
	}
	chatB, _, err := service.CreateConversation(ctx, accounts[1], core.ConversationTarget{Type: "phone_number", Value: "15551113333"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(native.New(service, "test-token"))
	imageData := testPNG(t)
	upload := uploadTestFile(t, server, accounts[0], imageData, "photo.png", "image/png", http.StatusCreated)
	if upload.MIMEType != "image/png" || upload.Size != int64(len(imageData)) {
		t.Fatalf("upload metadata: %+v", upload)
	}
	wrongAccount := request(t, server.Client(), http.MethodPost, server.URL+"/api/v1/conversations/"+chatB.ID+"/messages",
		mediaBody("image", upload.ID, ""), "wrong-account-key")
	wrongAccount.Body.Close()
	if wrongAccount.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-account send: %d", wrongAccount.StatusCode)
	}
	sendURL := server.URL + "/api/v1/conversations/" + chatA.ID + "/messages"
	sent := decode[struct {
		ID          string `json:"id"`
		Attachments []struct {
			ID           string `json:"id"`
			Availability string `json:"availability"`
		} `json:"attachments"`
	}](t, request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", upload.ID, "hello"), "media-send-key-a"), http.StatusAccepted)
	if len(sent.Attachments) != 1 || sent.Attachments[0].ID != upload.ID || sent.Attachments[0].Availability != "ready" {
		t.Fatalf("send response attachment: %+v", sent.Attachments)
	}
	waitFor(t, func() bool { m, err := repo.GetMessage(ctx, sent.ID); return err == nil && m.State == "sent" })
	if connector.mediaUploads.Load() != 1 || connector.mediaSends.Load() != 1 {
		t.Fatalf("provider calls: uploads=%d sends=%d", connector.mediaUploads.Load(), connector.mediaSends.Load())
	}
	duplicate := decode[struct {
		ID string `json:"id"`
	}](t,
		request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", upload.ID, "hello"), "media-send-key-a"), http.StatusAccepted)
	if duplicate.ID != sent.ID || connector.mediaSends.Load() != 1 {
		t.Fatal("duplicate request sent another message")
	}
	conflict := request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", upload.ID, "changed"), "media-send-key-a")
	conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("key conflict: %d", conflict.StatusCode)
	}
	consumed := request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", upload.ID, ""), "media-send-key-b")
	consumed.Body.Close()
	if consumed.StatusCode != http.StatusNotFound {
		t.Fatalf("consumed upload: %d", consumed.StatusCode)
	}
	content := request(t, server.Client(), http.MethodGet, server.URL+"/api/v1/attachments/"+upload.ID+"/content", nil, "")
	contentData, _ := io.ReadAll(content.Body)
	content.Body.Close()
	if content.StatusCode != http.StatusOK || !bytes.Equal(contentData, imageData) {
		t.Fatalf("sent content: status=%d", content.StatusCode)
	}

	unused := uploadTestFile(t, server, accounts[0], imageData, "unused.png", "image/png", http.StatusCreated)
	deleted := request(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/accounts/"+accounts[0]+"/uploads/"+unused.ID, nil, "")
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unused upload: %d", deleted.StatusCode)
	}
	deletedSend := request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", unused.ID, ""), "media-send-key-c")
	deletedSend.Body.Close()
	if deletedSend.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted upload was sendable: %d", deletedSend.StatusCode)
	}
	waitFor(t, func() bool {
		used, err := repo.StoredMediaBytes(ctx)
		return err == nil && used == int64(len(imageData))
	})
	uploadTestFile(t, server, accounts[0], bytes.Repeat([]byte("x"), 4097), "large.bin", "application/octet-stream", http.StatusRequestEntityTooLarge)
	uploadTestFile(t, server, accounts[0], imageData, "wrong.jpg", "image/jpeg", http.StatusBadRequest)

	connector.failMediaSend.Store(true)
	uncertainUpload := uploadTestFile(t, server, accounts[0], imageData, "uncertain.png", "image/png", http.StatusCreated)
	uncertain := decode[struct {
		ID string `json:"id"`
	}](t,
		request(t, server.Client(), http.MethodPost, sendURL, mediaBody("image", uncertainUpload.ID, ""), "media-send-key-d"), http.StatusAccepted)
	waitFor(t, func() bool {
		m, err := repo.GetMessage(ctx, uncertain.ID)
		return err == nil && m.State == "outcome_unknown"
	})
	if connector.mediaSends.Load() != 2 {
		t.Fatalf("expected one uncertain send, got %d total", connector.mediaSends.Load())
	}
	server.Close()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	connector.failMediaSend.Store(false)
	service = app.NewWithMedia(reopened, connector, nil, options)
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	waitFor(t, func() bool {
		status, err := service.Account(accounts[0])
		return err == nil && status.State == "connected"
	})
	if m, err := reopened.GetMessage(ctx, uncertain.ID); err != nil || m.State != "outcome_unknown" || connector.mediaSends.Load() != 2 {
		t.Fatalf("uncertain send restarted: state=%s sends=%d err=%v", m.State, connector.mediaSends.Load(), err)
	}
}

func TestUploadQuotaCountsUnusedFiles(t *testing.T) {
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
	local, err := media.NewLocal(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := media.NewRegistry("local", map[string]media.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewWithMedia(repo, &fakeConnector{}, nil, app.MediaOptions{Stores: registry, TempDir: filepath.Join(dir, "tmp"),
		MaxFileBytes: 10, MaxTotalBytes: 10, MaxTempBytes: (1 << 20) + 10, Workers: 1})
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(native.New(service, "test-token"))
	defer server.Close()
	first := uploadTestFile(t, server, accountID, []byte("123456"), "first.txt", "text/plain", http.StatusCreated)
	uploadTestFile(t, server, accountID, []byte("abcdef"), "second.txt", "text/plain", http.StatusInsufficientStorage)
	deleted := request(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/accounts/"+accountID+"/uploads/"+first.ID, nil, "")
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete upload: %d", deleted.StatusCode)
	}
	waitFor(t, func() bool { used, err := repo.StoredMediaBytes(ctx); return err == nil && used == 0 })
	uploadTestFile(t, server, accountID, []byte("abcdef"), "second.txt", "text/plain", http.StatusCreated)
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func mediaBody(kind, uploadID, caption string) any {
	return map[string]any{"kind": kind, "content": map[string]string{"upload_id": uploadID, "caption": caption}}
}

func uploadTestFile(t *testing.T, server *httptest.Server, accountID string, data []byte, name, contentType string, expected int) struct {
	ID       string `json:"id"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size"`
} {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="` + name + `"`}, "Content-Type": {contentType}}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/accounts/"+accountID+"/uploads", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return decode[struct {
		ID       string `json:"id"`
		MIMEType string `json:"mime_type"`
		Size     int64  `json:"size"`
	}](t, resp, expected)
}
