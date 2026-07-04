package telegram

import (
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

func TestNormalizeSyncLimit(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int
	}{
		{in: -1, want: 200},
		{in: 0, want: 200},
		{in: 50, want: 50},
		{in: 5000, want: 1000},
	} {
		if got := normalizeSyncLimit(tc.in); got != tc.want {
			t.Fatalf("normalizeSyncLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeBackfill(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int
	}{
		{in: -1, want: 0},
		{in: 0, want: 0},
		{in: 20, want: 20},
		{in: 5000, want: 1000},
	} {
		if got := normalizeBackfill(tc.in); got != tc.want {
			t.Fatalf("normalizeBackfill(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestExternalMessageID(t *testing.T) {
	got := externalMessageID("mpwb_chat", domain.SourceLabelPost, 26782)
	want := "telegram:POST:mpwb_chat:26782"
	if got != want {
		t.Fatalf("external id = %q, want %q", got, want)
	}
}

func TestMessageLink(t *testing.T) {
	source := domain.Source{EntityRef: "https://t.me/mpwb_chat"}
	got := messageLink(source, 26782)
	want := "https://t.me/mpwb_chat/26782"
	if got != want {
		t.Fatalf("link = %q, want %q", got, want)
	}
}

func TestMessageLinkSkipsInvite(t *testing.T) {
	source := domain.Source{EntityRef: "https://t.me/+secret"}
	if got := messageLink(source, 1); got != "" {
		t.Fatalf("link = %q, want empty", got)
	}
}

func TestPublicUsernamePrefersExplicitUsername(t *testing.T) {
	source := domain.Source{
		EntityRef:      "https://t.me/entity",
		PublicUsername: "@explicit",
	}
	if got := publicUsername(source); got != "explicit" {
		t.Fatalf("username = %q", got)
	}
}
