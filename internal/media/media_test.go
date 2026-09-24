package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestLocalStorageAtomicReplacementAndRange(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "media")
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := Key(uuid.NewString(), uuid.NewString())
	staged := stage(t, "first")
	defer staged.Close()
	if err := store.Put(ctx, key, staged, 5); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(key))
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("stored permissions: %v, %v", info, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	replacement := stage(t, "second")
	defer replacement.Close()
	if err := store.Put(canceled, key, replacement, 6); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replacement: %v", err)
	}
	reader, err := store.Open(ctx, key, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader)
	reader.Close()
	if string(data) != "irs" {
		t.Fatalf("stored range changed: %q", data)
	}
	if err := store.Put(ctx, key, replacement, 6); err != nil {
		t.Fatal(err)
	}
	reader, err = store.Open(ctx, key, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(reader)
	reader.Close()
	if string(data) != "second" {
		t.Fatalf("replacement: %q", data)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(ctx, key, 0, -1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted object: %v", err)
	}
	if err := store.Delete(ctx, "../../outside"); err == nil {
		t.Fatal("invalid object key was accepted")
	}
}

func TestS3CompatibleOperations(t *testing.T) {
	key, _ := Key(uuid.NewString(), uuid.NewString())
	path := "/bucket/" + key
	var saved string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("object path: %s", r.URL.Path)
			http.Error(w, "wrong path", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("X-Amz-Sdk-Checksum-Algorithm") != "" || r.Header.Get("X-Amz-Checksum-Crc32") != "" {
				t.Errorf("unsupported checksum headers: %v", r.Header)
			}
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			saved = string(body)
			mu.Unlock()
			w.Header().Set("ETag", `"test"`)
		case http.MethodGet:
			mu.Lock()
			defer mu.Unlock()
			if saved == "" {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
				return
			}
			if r.Header.Get("Range") != "bytes=1-3" {
				t.Errorf("range header: %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 1-3/5")
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, saved[1:4])
		case http.MethodDelete:
			mu.Lock()
			saved = ""
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	store, err := NewS3(context.Background(), S3Options{Bucket: "bucket", Region: "auto", Endpoint: server.URL,
		AccessKey: "test-key", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	file := stage(t, "hello")
	defer file.Close()
	if err := store.Put(context.Background(), key, file, 5); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	uploaded := saved
	mu.Unlock()
	if uploaded != "hello" {
		t.Fatalf("uploaded body: %q", uploaded)
	}
	reader, err := store.Open(context.Background(), key, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader)
	reader.Close()
	if string(data) != "ell" {
		t.Fatalf("downloaded range: %q", data)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), key, 1, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing object: %v", err)
	}
}

func stage(t *testing.T, content string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stage-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	return file
}
