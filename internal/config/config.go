// Package config loads runtime configuration.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultDataDir    = "data"
	defaultListenAddr = "127.0.0.1:1984"
)

// Config contains runtime paths and non-secret settings.
type Config struct {
	DataDir            string
	DatabasePath       string
	TelegramSessionDir string
	ListenAddr         string
	PublicBaseURL      string
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

	listenAddr := strings.TrimSpace(os.Getenv("TGMCP_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	publicBaseURL := firstNonEmptyEnv("TGMCP_PUBLIC_BASE_URL", "MCP_PUBLIC_BASE_URL")

	return Config{
		DataDir:            dataDir,
		DatabasePath:       databasePath,
		TelegramSessionDir: sessionDir,
		ListenAddr:         listenAddr,
		PublicBaseURL:      publicBaseURL,
	}
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	return ""
}
