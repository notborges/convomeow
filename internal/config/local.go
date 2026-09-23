package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	Dir         string
	AppDB       string
	WhatsAppDB  string
	Token       string
	ProcessLock string
}

func DataPaths(dir string) (Paths, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Paths{}, err
	}
	return Paths{Dir: abs, AppDB: filepath.Join(abs, "app.sqlite"), WhatsAppDB: filepath.Join(abs, "whatsmeow.sqlite"),
		Token: filepath.Join(abs, "control.token"), ProcessLock: filepath.Join(abs, "service.lock")}, nil
}

func (p Paths) EnsureDir() error {
	if err := os.MkdirAll(p.Dir, 0700); err != nil {
		return err
	}
	return os.Chmod(p.Dir, 0700)
}

func (p Paths) CreateOrReadToken() (string, error) {
	if token, err := p.ReadToken(); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	f, err := os.OpenFile(p.Token, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(p.Token)
		}
	}()
	if _, err := f.WriteString(token + "\n"); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	created = false
	return token, nil
}

func (p Paths) ReadToken() (string, error) {
	data, err := os.ReadFile(p.Token)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("invalid control token: %s", p.Token)
	}
	return token, nil
}
