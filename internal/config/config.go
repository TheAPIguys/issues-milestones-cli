package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const configDirectory = "i-gh"

type Config struct {
	LastRepository string `json:"last_repository,omitempty"`
	path           string
}

func Load() (Config, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("find user config directory: %w", err)
	}

	path := filepath.Join(directory, configDirectory, "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{path: path}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	var stored Config
	if err := json.Unmarshal(data, &stored); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	stored.path = path
	return stored, nil
}

func (c *Config) SetLastRepository(repository string) error {
	c.LastRepository = repository
	if c.path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(c.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", c.path, err)
	}
	return nil
}
