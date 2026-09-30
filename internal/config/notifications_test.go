package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNotificationConfigAndPersistentKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("notifications:\n  enabled: false\n  contact: mailto:admin@example.com\nmedia:\n  workers: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadNotifications(path)
	if err != nil || *cfg.Enabled || cfg.Contact != "mailto:admin@example.com" {
		t.Fatalf("config: %+v %v", cfg, err)
	}
	if _, err := LoadMedia(path, dir); err != nil {
		t.Fatal(err)
	}
	first, err := LoadVAPIDKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadVAPIDKeys(dir)
	if err != nil || first != second {
		t.Fatal("VAPID identity changed on restart")
	}
	info, err := os.Stat(filepath.Join(dir, "push-vapid.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("VAPID private key has unsafe permissions")
	}
	if err := os.WriteFile(filepath.Join(dir, "push-vapid.json"), []byte(`{"public":"invalid","private":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVAPIDKeys(dir); err == nil {
		t.Fatal("invalid key file was silently replaced")
	}
}
