package config

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	push "github.com/SherClockHolmes/webpush-go"
)

type NotificationsConfig struct {
	Enabled *bool  `yaml:"enabled"`
	Contact string `yaml:"contact"`
}

type VAPIDKeys struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

func LoadNotifications(path string) (NotificationsConfig, error) {
	enabled := true
	cfg := NotificationsConfig{Enabled: &enabled, Contact: "https://github.com/notborges/convomeow"}
	input, err := readConfig(path)
	if err != nil {
		return cfg, err
	}
	if input.Notifications != nil {
		if input.Notifications.Enabled != nil {
			cfg.Enabled = input.Notifications.Enabled
		}
		if input.Notifications.Contact != "" {
			cfg.Contact = input.Notifications.Contact
		}
	}
	u, err := url.Parse(cfg.Contact)
	if err != nil || !(u.Scheme == "https" && u.Hostname() != "" || u.Scheme == "mailto" && u.Opaque != "") {
		return cfg, errors.New("notification contact must be an HTTPS or mailto URL")
	}
	return cfg, nil
}

func LoadVAPIDKeys(dataDir string) (VAPIDKeys, error) {
	path := filepath.Join(dataDir, "push-vapid.json")
	var keys VAPIDKeys
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &keys); err != nil {
			return keys, errors.New("invalid VAPID key file")
		}
		private, err := base64.RawURLEncoding.DecodeString(keys.Private)
		if err != nil {
			return keys, errors.New("invalid VAPID private key")
		}
		key, err := ecdh.P256().NewPrivateKey(private)
		public, decodeErr := base64.RawURLEncoding.DecodeString(keys.Public)
		if err != nil || decodeErr != nil || !bytes.Equal(key.PublicKey().Bytes(), public) {
			return keys, errors.New("invalid VAPID key pair")
		}
		return keys, os.Chmod(path, 0600)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return keys, err
	}
	keys.Private, keys.Public, err = push.GenerateVAPIDKeys()
	if err != nil {
		return keys, err
	}
	data, err = json.Marshal(keys)
	if err != nil {
		return keys, err
	}
	f, err := os.CreateTemp(dataDir, ".push-vapid-*")
	if err != nil {
		return keys, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return keys, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return keys, err
	}
	if err = f.Close(); err != nil {
		return keys, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return keys, fmt.Errorf("save VAPID keys: %w", err)
	}
	return keys, nil
}
