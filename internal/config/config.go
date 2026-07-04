// Package config loads runtime configuration.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultDataDir = "data"
)

// Config contains runtime paths and non-secret settings.
type Config struct {
	DataDir            string
	DatabasePath       string
	TelegramSessionDir string
}

// LoadFromEnv reads configuration from environment variables.
func LoadFromEnv() Config {
	dataDir := strings.TrimSpace(os.Getenv("TGMCP_DATA_DIR"))
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	databasePath := strings.TrimSpace(os.Getenv("TGMCP_DATABASE_PATH"))
	if databasePath == "" {
		databasePath = filepath.Join(dataDir, "telegram-mcp.sqlite")
	}

	sessionDir := strings.TrimSpace(os.Getenv("TGMCP_TELEGRAM_SESSION_DIR"))
	if sessionDir == "" {
		sessionDir = filepath.Join(dataDir, "session")
	}

	return Config{
		DataDir:            dataDir,
		DatabasePath:       databasePath,
		TelegramSessionDir: sessionDir,
	}
}
