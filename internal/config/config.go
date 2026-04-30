// Package config persists the CLI's pairing state on disk.
//
// Files (per OS):
//
//	macOS / Linux: ~/.config/ee-tunnel/config.json
//	               ~/.config/ee-tunnel/token       (mode 0600)
//	Windows:       %APPDATA%\ee-tunnel\config.json
//	               %APPDATA%\ee-tunnel\token
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	configFileName = "config.json"
	tokenFileName  = "token"
	dirPerm        = 0o700
	tokenPerm      = 0o600
)

// Config holds non-secret tunnel state. The refresh token lives separately in
// `token` so it can be excluded from accidental copies of config.json.
type Config struct {
	TunnelID  string `json:"tunnel_id"`
	OrgSlug   string `json:"org_slug"`
	Label     string `json:"label,omitempty"`
	ServerURL string `json:"server_url"`
	OllamaURL string `json:"ollama_url,omitempty"`
}

func dir() (string, error) {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("APPDATA environment variable not set")
		}
		return filepath.Join(appData, "ee-tunnel"), nil
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("UserConfigDir: %w", err)
	}
	return filepath.Join(cfgDir, "ee-tunnel"), nil
}

// Path returns the directory used for ee-tunnel state.
func Path() (string, error) {
	return dir()
}

// Save writes config.json + token to disk, creating the directory if needed.
// The token is written with 0600 permissions on POSIX systems.
func Save(cfg *Config, refreshToken string) error {
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", d, err)
	}

	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(d, configFileName), cfgBytes, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if err := os.WriteFile(filepath.Join(d, tokenFileName), []byte(refreshToken), tokenPerm); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	return nil
}

// Load reads config.json + token from disk. Returns ErrNotPaired if the CLI
// has never been paired on this machine.
var ErrNotPaired = errors.New("ee-tunnel is not paired on this machine; run 'ee-tunnel pair <refresh-token>' first")

func Load() (*Config, string, error) {
	d, err := dir()
	if err != nil {
		return nil, "", err
	}
	cfgBytes, err := os.ReadFile(filepath.Join(d, configFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotPaired
	}
	if err != nil {
		return nil, "", fmt.Errorf("read config: %w", err)
	}
	tokenBytes, err := os.ReadFile(filepath.Join(d, tokenFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotPaired
	}
	if err != nil {
		return nil, "", fmt.Errorf("read token: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, "", fmt.Errorf("parse config: %w", err)
	}
	return &cfg, string(tokenBytes), nil
}

// Clear removes the on-disk pairing state. Used by `ee-tunnel disconnect`.
func Clear() error {
	d, err := dir()
	if err != nil {
		return err
	}
	for _, name := range []string{configFileName, tokenFileName} {
		path := filepath.Join(d, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
