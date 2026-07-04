package config

import "testing"

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("TGMCP_DATA_DIR", "")
	t.Setenv("TGMCP_DATABASE_PATH", "")
	t.Setenv("TGMCP_TELEGRAM_SESSION_DIR", "")

	cfg := LoadFromEnv()

	if cfg.DataDir != "data" {
		t.Fatalf("data dir = %q, want data", cfg.DataDir)
	}
	if cfg.DatabasePath != "data/telegram-mcp.sqlite" {
		t.Fatalf("database path = %q", cfg.DatabasePath)
	}
	if cfg.TelegramSessionDir != "data/session" {
		t.Fatalf("session dir = %q", cfg.TelegramSessionDir)
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	t.Setenv("TGMCP_DATA_DIR", "/tmp/tgmcp")
	t.Setenv("TGMCP_DATABASE_PATH", "/tmp/custom.sqlite")
	t.Setenv("TGMCP_TELEGRAM_SESSION_DIR", "/tmp/session")

	cfg := LoadFromEnv()

	if cfg.DataDir != "/tmp/tgmcp" {
		t.Fatalf("data dir = %q", cfg.DataDir)
	}
	if cfg.DatabasePath != "/tmp/custom.sqlite" {
		t.Fatalf("database path = %q", cfg.DatabasePath)
	}
	if cfg.TelegramSessionDir != "/tmp/session" {
		t.Fatalf("session dir = %q", cfg.TelegramSessionDir)
	}
}
