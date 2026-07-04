package mcp

import "testing"

func TestListTools(t *testing.T) {
	tools := ListTools()
	if len(tools) != 11 {
		t.Fatalf("tool count = %d, want 11", len(tools))
	}

	required := map[string]bool{
		"telegram.sources_list":          false,
		"telegram.sync":                  false,
		"telegram.messages_recent":       false,
		"telegram.spam_add_from_message": false,
	}

	for _, tool := range tools {
		if _, ok := required[tool.Name]; ok {
			required[tool.Name] = true
		}
		if tool.Name == "" {
			t.Fatal("tool name is empty")
		}
		if tool.Description == "" {
			t.Fatalf("tool %s description is empty", tool.Name)
		}
	}

	for name, found := range required {
		if !found {
			t.Fatalf("required tool %s not found", name)
		}
	}
}
