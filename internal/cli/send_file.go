package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/notborges/convomeow/internal/core"
)

func (c *Client) sendFile(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("message send-file", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	key := flags.String("key", uuid.NewString(), "")
	caption := flags.String("caption", "", "")
	uploadID := flags.String("upload-id", "", "")
	if err := flags.Parse(args); err != nil {
		return usage()
	}
	positional := flags.Args()
	if len(positional) != 4 && !(len(positional) == 3 && *uploadID != "") {
		return usage()
	}
	account, err := c.resolve(ctx, positional[0])
	if err != nil {
		return err
	}
	conversationID, err := c.resolveConversation(ctx, account.ID, positional[1])
	if err != nil {
		return err
	}
	kind := core.MessageKind(positional[2])
	if kind != core.MessageKindImage && kind != core.MessageKindVideo && kind != core.MessageKindAudio &&
		kind != core.MessageKindDocument && kind != core.MessageKindSticker {
		return errors.New("kind must be image, video, audio, document, or sticker")
	}
	if *uploadID == "" {
		if len(positional) != 4 {
			return usage()
		}
		*uploadID, err = c.uploadFile(ctx, account.ID, positional[3])
		if err != nil {
			return err
		}
	}
	path := "/api/v1/conversations/" + url.PathEscape(conversationID) + "/messages"
	body := map[string]any{"kind": kind, "content": map[string]string{"upload_id": *uploadID, "caption": *caption}}
	var sent message
	if err := c.do(ctx, http.MethodPost, path, body, &sent, *key); err != nil {
		return fmt.Errorf("%w (retry with --key %s --upload-id %s)", err, *key, *uploadID)
	}
	fmt.Printf("Message %s: %s\n", sent.ID, sent.State)
	return nil
}

func (c *Client) resolveConversation(ctx context.Context, accountID, target string) (string, error) {
	if _, err := uuid.Parse(target); err == nil {
		var existing conversation
		if err := c.do(ctx, http.MethodGet, "/api/v1/conversations/"+url.PathEscape(target), nil, &existing); err != nil {
			return "", err
		}
		if existing.AccountID != accountID {
			return "", fmt.Errorf("conversation belongs to account %s", existing.AccountID)
		}
		return existing.ID, nil
	}
	var created conversation
	body := map[string]any{"target": map[string]string{"type": "phone_number", "value": target}}
	if err := c.do(ctx, http.MethodPost, "/api/v1/accounts/"+url.PathEscape(accountID)+"/conversations", body, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) uploadFile(ctx context.Context, accountID, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		file.Close()
		return "", errors.New("file must be a non-empty regular file")
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	path := "/api/v1/accounts/" + url.PathEscape(accountID) + "/uploads"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		file.Close()
		reader.Close()
		writer.Close()
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	done := make(chan error, 1)
	go func() {
		defer file.Close()
		name := filepath.Base(filePath)
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
		header.Set("Content-Type", contentType)
		part, err := multipartWriter.CreatePart(header)
		if err == nil {
			_, err = io.Copy(part, file)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	uploadClient := *c.http
	uploadClient.Timeout = 10 * time.Minute
	resp, err := uploadClient.Do(req)
	if err != nil {
		reader.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		reader.Close()
		return "", decodeResponse(resp, nil)
	}
	if err := <-done; err != nil {
		return "", err
	}
	var upload struct {
		ID string `json:"id"`
	}
	if err := decodeResponse(resp, &upload); err != nil {
		return "", err
	}
	return upload.ID, nil
}
