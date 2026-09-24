package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaConfigProfilesAndLimits(t *testing.T) {
	dir := t.TempDir()
	defaultConfig, err := LoadMedia(filepath.Join(dir, "absent.yaml"), dir)
	if err != nil || defaultConfig.ActiveProfile != "local" || defaultConfig.Profiles["local"].Path != filepath.Join(dir, "media") {
		t.Fatalf("default media config: %+v, %v", defaultConfig, err)
	}
	t.Setenv("TEST_MEDIA_ACCESS", "access")
	t.Setenv("TEST_MEDIA_SECRET", "secret")
	path := filepath.Join(dir, "config.yaml")
	content := `media:
  active_profile: r2
  max_file_bytes: 1024
  max_total_bytes: 2048
  max_temp_bytes: 1049600
  workers: 1
  profiles:
    local:
      driver: local
      path: archive
    r2:
      driver: s3
      bucket: example
      region: auto
      endpoint: https://example.r2.cloudflarestorage.com
      access_key_env: TEST_MEDIA_ACCESS
      secret_key_env: TEST_MEDIA_SECRET
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMedia(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "r2" || cfg.Profiles["local"].Path != filepath.Join(dir, "archive") ||
		cfg.Profiles["r2"].AccessKey != "access" || cfg.Profiles["r2"].SecretKey != "secret" {
		t.Fatalf("parsed media profiles: %+v", cfg)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, "https://example", "http://example", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMedia(path, dir); err == nil {
		t.Fatal("insecure S3 endpoint was accepted")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, "max_temp_bytes: 1049600", "max_temp_bytes: 1024", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMedia(path, dir); err == nil {
		t.Fatal("staging limit below one file was accepted")
	}
}
