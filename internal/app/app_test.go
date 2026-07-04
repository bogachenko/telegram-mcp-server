package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/config"
)

func TestPrintStatus(t *testing.T) {
	application := New(config.Config{
		DataDir:            "data",
		DatabasePath:       "data/telegram-mcp.sqlite",
		TelegramSessionDir: "data/session",
	})

	var output bytes.Buffer
	if err := application.PrintStatus(&output); err != nil {
		t.Fatalf("print status: %v", err)
	}

	got := output.String()
	for _, want := range []string{
		"telegram-mcp-server configured",
		"data dir: data",
		"database path: data/telegram-mcp.sqlite",
		"telegram session dir: data/session",
		"mcp tools planned:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output does not contain %q:\n%s", want, got)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run([]string{"bad"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `unknown command "bad"`) {
		t.Fatalf("error = %q", err)
	}
}
