// Package config loads runtime configuration.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultDataDir    = "data"
	defaultListenAddr = "127.0.0.1:1984"
)

// Config contains runtime paths and non-secret settings.
type Config struct {
	DataDir             string
	DatabasePath        string
	TelegramSessionDir  string
	TelegramSessionPath string
	TelegramAPIID       int
	TelegramAPIHash     string
	TelegramPhone       string
	TelegramPassword    string
	ListenAddr          string
	PublicBaseURL       string
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

	sessionPath := strings.TrimSpace(os.Getenv("TGMCP_TELEGRAM_SESSION_PATH"))
	if sessionPath == "" {
		sessionPath = filepath.Join(sessionDir, "session.json")
	}

	listenAddr := strings.TrimSpace(os.Getenv("TGMCP_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	return Config{
		DataDir:             dataDir,
		DatabasePath:        databasePath,
		TelegramSessionDir:  sessionDir,
		TelegramSessionPath: sessionPath,
		TelegramAPIID:       intFromEnv("TGMCP_TELEGRAM_API_ID"),
		TelegramAPIHash:     strings.TrimSpace(os.Getenv("TGMCP_TELEGRAM_API_HASH")),
		TelegramPhone:       strings.TrimSpace(os.Getenv("TGMCP_TELEGRAM_PHONE")),
		TelegramPassword:    strings.TrimSpace(os.Getenv("TGMCP_TELEGRAM_PASSWORD")),
		ListenAddr:          listenAddr,
		PublicBaseURL:       firstNonEmptyEnv("TGMCP_PUBLIC_BASE_URL", "MCP_PUBLIC_BASE_URL"),
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

func intFromEnv(name string) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
