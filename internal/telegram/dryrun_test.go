package telegram

import (
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/gotd/td/tg"
)

func TestNormalizeDryRunLimit(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int
	}{
		{in: -1, want: 5},
		{in: 0, want: 5},
		{in: 7, want: 7},
		{in: 100, want: 50},
	} {
		if got := normalizeDryRunLimit(tc.in); got != tc.want {
			t.Fatalf("normalizeDryRunLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSourceResolveRef(t *testing.T) {
	source := domain.Source{
		ID:             "local-id",
		PublicUsername: "public",
		EntityRef:      "@entity",
	}
	if got := sourceResolveRef(source); got != "@entity" {
		t.Fatalf("ref = %q", got)
	}

	source.EntityRef = ""
	if got := sourceResolveRef(source); got != "public" {
		t.Fatalf("ref = %q", got)
	}

	source.PublicUsername = ""
	if got := sourceResolveRef(source); got != "local-id" {
		t.Fatalf("ref = %q", got)
	}
}

func TestPeerIDString(t *testing.T) {
	for _, tc := range []struct {
		peer tg.PeerClass
		want string
	}{
		{peer: &tg.PeerUser{UserID: 1}, want: "user:1"},
		{peer: &tg.PeerChat{ChatID: 2}, want: "chat:2"},
		{peer: &tg.PeerChannel{ChannelID: 3}, want: "channel:3"},
	} {
		if got := peerIDString(tc.peer); got != tc.want {
			t.Fatalf("peerIDString(%T) = %q, want %q", tc.peer, got, tc.want)
		}
	}
}
