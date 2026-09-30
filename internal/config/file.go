package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Media         *MediaConfig         `yaml:"media"`
	Notifications *NotificationsConfig `yaml:"notifications"`
}

func readConfig(path string) (fileConfig, error) {
	var input fileConfig
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return input, nil
	}
	if err != nil {
		return input, err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("read config: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("configuration must contain one document")
		}
		return input, err
	}
	return input, nil
}
