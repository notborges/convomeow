package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxSingleUpload = 5<<30 - 1

type MediaConfig struct {
	ActiveProfile string                  `yaml:"active_profile"`
	MaxFileBytes  int64                   `yaml:"max_file_bytes"`
	MaxTotalBytes int64                   `yaml:"max_total_bytes"`
	MaxTempBytes  int64                   `yaml:"max_temp_bytes"`
	Workers       int                     `yaml:"workers"`
	Profiles      map[string]MediaProfile `yaml:"profiles"`
}

type MediaProfile struct {
	Driver       string `yaml:"driver"`
	Path         string `yaml:"path"`
	Bucket       string `yaml:"bucket"`
	Region       string `yaml:"region"`
	Endpoint     string `yaml:"endpoint"`
	AccessKeyEnv string `yaml:"access_key_env"`
	SecretKeyEnv string `yaml:"secret_key_env"`
	AccessKey    string `yaml:"-"`
	SecretKey    string `yaml:"-"`
}

func DefaultMediaConfig(dataDir string) MediaConfig {
	return MediaConfig{
		ActiveProfile: "local",
		MaxFileBytes:  256 << 20,
		MaxTotalBytes: 10 << 30,
		MaxTempBytes:  1 << 30,
		Workers:       2,
		Profiles: map[string]MediaProfile{
			"local": {Driver: "local", Path: filepath.Join(dataDir, "media")},
		},
	}
}

func LoadMedia(path, dataDir string) (MediaConfig, error) {
	cfg := DefaultMediaConfig(dataDir)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return MediaConfig{}, err
	}
	defer f.Close()
	var input struct {
		Media *MediaConfig `yaml:"media"`
	}
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&input); err != nil {
		return MediaConfig{}, fmt.Errorf("read media config: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return MediaConfig{}, errors.New("media config must contain one document")
		}
		return MediaConfig{}, err
	}
	if input.Media == nil {
		return cfg, nil
	}
	media := input.Media
	if media.ActiveProfile != "" {
		cfg.ActiveProfile = media.ActiveProfile
	}
	if media.MaxFileBytes != 0 {
		cfg.MaxFileBytes = media.MaxFileBytes
	}
	if media.MaxTotalBytes != 0 {
		cfg.MaxTotalBytes = media.MaxTotalBytes
	}
	if media.MaxTempBytes != 0 {
		cfg.MaxTempBytes = media.MaxTempBytes
	}
	if media.Workers != 0 {
		cfg.Workers = media.Workers
	}
	for id, profile := range media.Profiles {
		cfg.Profiles[id] = profile
	}
	if err := cfg.validate(dataDir); err != nil {
		return MediaConfig{}, err
	}
	return cfg, nil
}

func (c *MediaConfig) validate(dataDir string) error {
	if c.MaxFileBytes < 1 || c.MaxFileBytes > maxSingleUpload {
		return fmt.Errorf("max_file_bytes must be between 1 and %d", maxSingleUpload)
	}
	if c.MaxTotalBytes < c.MaxFileBytes || c.MaxTempBytes < c.MaxFileBytes+(1<<20) {
		return errors.New("media total and temporary limits must allow one maximum-size file")
	}
	if c.Workers < 1 || c.Workers > 16 {
		return errors.New("media workers must be between 1 and 16")
	}
	if _, ok := c.Profiles[c.ActiveProfile]; !ok {
		return fmt.Errorf("active media profile %q is not configured", c.ActiveProfile)
	}
	for id, profile := range c.Profiles {
		if !validProfileID(id) {
			return fmt.Errorf("invalid media profile ID %q", id)
		}
		switch profile.Driver {
		case "local":
			if profile.Bucket != "" || profile.Region != "" || profile.Endpoint != "" || profile.AccessKeyEnv != "" || profile.SecretKeyEnv != "" {
				return fmt.Errorf("local media profile %q has S3 settings", id)
			}
			if profile.Path == "" {
				profile.Path = filepath.Join(dataDir, "media", id)
				if id == "local" {
					profile.Path = filepath.Join(dataDir, "media")
				}
			} else if !filepath.IsAbs(profile.Path) {
				profile.Path = filepath.Join(dataDir, profile.Path)
			}
			absolute, err := filepath.Abs(profile.Path)
			if err != nil {
				return err
			}
			profile.Path = absolute
		case "s3":
			if profile.Path != "" || profile.Bucket == "" || profile.Region == "" {
				return fmt.Errorf("S3 media profile %q requires bucket and region, without a local path", id)
			}
			if profile.Endpoint != "" {
				endpoint, err := url.Parse(profile.Endpoint)
				if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
					return fmt.Errorf("S3 media profile %q requires an HTTPS endpoint", id)
				}
			}
			if (profile.AccessKeyEnv == "") != (profile.SecretKeyEnv == "") {
				return fmt.Errorf("S3 media profile %q requires both credential environment names", id)
			}
			if profile.Endpoint != "" && profile.AccessKeyEnv == "" {
				return fmt.Errorf("S3 media profile %q requires credential environment names with a custom endpoint", id)
			}
			if profile.AccessKeyEnv != "" {
				profile.AccessKey, profile.SecretKey = os.Getenv(profile.AccessKeyEnv), os.Getenv(profile.SecretKeyEnv)
				if profile.AccessKey == "" || profile.SecretKey == "" {
					return fmt.Errorf("S3 media profile %q has missing credential environment values", id)
				}
			}
		default:
			return fmt.Errorf("media profile %q has unsupported driver %q", id, profile.Driver)
		}
		c.Profiles[id] = profile
	}
	return nil
}

func validProfileID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", r) {
			return false
		}
	}
	return true
}
