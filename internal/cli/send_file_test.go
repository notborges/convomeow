package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadFileStreamsMultipart(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("report body"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/account-1/uploads" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request path or authorization: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		multipartReader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		part, err := multipartReader.NextPart()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		content, err := io.ReadAll(part)
		if err != nil || part.FormName() != "file" || part.FileName() != "report.txt" || string(content) != "report body" {
			t.Errorf("file part: name=%s filename=%s content=%q error=%v", part.FormName(), part.FileName(), content, err)
		}
		if _, err := multipartReader.NextPart(); err != io.EOF {
			t.Errorf("extra part or truncated body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "upload-1"})
	}))
	defer server.Close()
	client := New(server.URL, "secret")
	id, err := client.uploadFile(context.Background(), "account-1", filePath)
	if err != nil || id != "upload-1" {
		t.Fatalf("upload: id=%s error=%v", id, err)
	}
}
