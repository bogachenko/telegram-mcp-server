package telegram

import (
	"strings"
	"testing"
)

func TestSelfDisplayName(t *testing.T) {
	self := Self{FirstName: "Ivan", LastName: "Andreevich"}

	if got := self.DisplayName(); got != "Ivan Andreevich" {
		t.Fatalf("display name = %q", got)
	}
}

func TestReadLineTrims(t *testing.T) {
	got, err := readLine(strings.NewReader("12345\n"))
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	if got != "12345" {
		t.Fatalf("line = %q", got)
	}
}

func TestValidateAuthRequiresPhone(t *testing.T) {
	cfg := Config{
		APIID:       1,
		APIHash:     "hash",
		SessionPath: "session.json",
	}

	err := cfg.validateAuth()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "TGMCP_TELEGRAM_PHONE") {
		t.Fatalf("error = %q", err)
	}
}

func TestValidateBaseRequiresAPIID(t *testing.T) {
	cfg := Config{
		APIHash:     "hash",
		SessionPath: "session.json",
	}

	err := cfg.validateBase()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "TGMCP_TELEGRAM_API_ID") {
		t.Fatalf("error = %q", err)
	}
}

func TestValidateBaseRequiresAPIHash(t *testing.T) {
	cfg := Config{
		APIID:       1,
		SessionPath: "session.json",
	}

	err := cfg.validateBase()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "TGMCP_TELEGRAM_API_HASH") {
		t.Fatalf("error = %q", err)
	}
}
