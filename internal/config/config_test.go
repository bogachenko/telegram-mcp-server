package config

import "testing"

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("TGMCP_DATA_DIR", "")
	t.Setenv("TGMCP_DATABASE_PATH", "")
	t.Setenv("TGMCP_TELEGRAM_SESSION_DIR", "")
	t.Setenv("TGMCP_LISTEN_ADDR", "")
	t.Setenv("TGMCP_PUBLIC_BASE_URL", "")
	t.Setenv("MCP_PUBLIC_BASE_URL", "")

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
	if cfg.ListenAddr != "127.0.0.1:1984" {
		t.Fatalf("listen addr = %q", cfg.ListenAddr)
	}
	if cfg.PublicBaseURL != "" {
		t.Fatalf("public base URL = %q, want empty", cfg.PublicBaseURL)
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	t.Setenv("TGMCP_DATA_DIR", "/tmp/tgmcp")
	t.Setenv("TGMCP_DATABASE_PATH", "/tmp/custom.sqlite")
	t.Setenv("TGMCP_TELEGRAM_SESSION_DIR", "/tmp/session")
	t.Setenv("TGMCP_LISTEN_ADDR", "127.0.0.1:1999")
	t.Setenv("TGMCP_PUBLIC_BASE_URL", "https://tg-mcp.elektrosila-avtomatika.store/")

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
	if cfg.ListenAddr != "127.0.0.1:1999" {
		t.Fatalf("listen addr = %q", cfg.ListenAddr)
	}
	if cfg.PublicBaseURL != "https://tg-mcp.elektrosila-avtomatika.store" {
		t.Fatalf("public base URL = %q", cfg.PublicBaseURL)
	}
}

func TestLoadFromEnvAcceptsGenericMCPPublicBaseURL(t *testing.T) {
	t.Setenv("TGMCP_PUBLIC_BASE_URL", "")
	t.Setenv("MCP_PUBLIC_BASE_URL", "https://fallback.example.com/")

	cfg := LoadFromEnv()

	if cfg.PublicBaseURL != "https://fallback.example.com" {
		t.Fatalf("public base URL = %q", cfg.PublicBaseURL)
	}
}
