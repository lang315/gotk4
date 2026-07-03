// config.go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
}

func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gotk4-telegram"), nil
}

func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gotk4-telegram"), nil
}

func path(dir string) string { return filepath.Join(dir, "config.json") }

func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(path(dir))
	if err != nil {
		return nil, err // wraps fs.ErrNotExist
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(dir), b, 0o600)
}
