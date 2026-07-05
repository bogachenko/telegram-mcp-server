package mcp

import (
	"encoding/json"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

func TestResourceURIParsers(t *testing.T) {
	sourceID, ok := sourceMessagesResourceID("telegram://source/mpwb_chat/messages")
	if !ok || sourceID != "mpwb_chat" {
		t.Fatalf("source id = %q, %t", sourceID, ok)
	}

	externalID, ok := messageResourceExternalID("telegram://message/telegram:POST:mpwb_chat:26782")
	if !ok || externalID != "telegram:POST:mpwb_chat:26782" {
		t.Fatalf("external id = %q, %t", externalID, ok)
	}

	sourceID, ok = spamListSourceResourceID("telegram://spam-list/source/mpwb_chat")
	if !ok || sourceID != "mpwb_chat" {
		t.Fatalf("spam source id = %q, %t", sourceID, ok)
	}

	externalID, ok = messagePathResourceExternalID("telegram://message/POST/mpwb_chat/26782")
	if !ok || externalID != "telegram:POST:mpwb_chat:26782" {
		t.Fatalf("path external id = %q, %t", externalID, ok)
	}

	if _, ok := messagePathResourceExternalID("telegram://message/BAD/mpwb_chat/26782"); ok {
		t.Fatal("bad source label parsed successfully")
	}

	if _, ok := sourceMessagesResourceID("telegram://source/mpwb_chat"); ok {
		t.Fatal("source messages URI without /messages parsed successfully")
	}
}

func TestJSONResource(t *testing.T) {
	result, err := jsonResource("telegram://sources", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("json resource: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("contents len = %d, want 1", len(result.Contents))
	}
	if result.Contents[0].URI != "telegram://sources" {
		t.Fatalf("uri = %q", result.Contents[0].URI)
	}
	if result.Contents[0].MIMEType != resourceMIMEJSON {
		t.Fatalf("mime = %q", result.Contents[0].MIMEType)
	}

	var decoded map[string]bool
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &decoded); err != nil {
		t.Fatalf("resource json invalid: %v", err)
	}
	if !decoded["ok"] {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestFilterExcludedSenders(t *testing.T) {
	items := []domain.ExcludedSender{
		{ID: 1, Scope: domain.ExclusionScopeGlobal},
		{ID: 2, Scope: domain.ExclusionScopeSource, SourceID: "mpwb_chat"},
		{ID: 3, Scope: domain.ExclusionScopeSource, SourceID: "other"},
	}

	filtered := filterExcludedSenders(items, func(item domain.ExcludedSender) bool {
		return item.Scope == domain.ExclusionScopeSource && item.SourceID == "mpwb_chat"
	})
	if len(filtered) != 1 || filtered[0].ID != 2 {
		t.Fatalf("filtered = %+v", filtered)
	}
}
