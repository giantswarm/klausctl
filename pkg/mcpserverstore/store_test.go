package mcpserverstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "mcpservers.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names := store.List(); len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestAddGetRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcpservers.yaml")
	store, _ := Load(path)

	store.Add("muster", McpServerDef{URL: "https://muster.example.com/mcp", Secret: "muster-token"})
	store.Add("other", McpServerDef{URL: "https://other.example.com/mcp"})

	def, err := store.Get("muster")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if def.URL != "https://muster.example.com/mcp" {
		t.Errorf("URL = %q, want muster URL", def.URL)
	}
	if def.Secret != "muster-token" {
		t.Errorf("Secret = %q, want muster-token", def.Secret)
	}

	names := store.List()
	if len(names) != 2 {
		t.Fatalf("List = %v, want 2 entries", names)
	}

	if err := store.Remove("muster"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := store.Get("muster"); err == nil {
		t.Error("expected error for removed server")
	}
}

func TestGetNotFound(t *testing.T) {
	store, _ := Load(filepath.Join(t.TempDir(), "mcpservers.yaml"))
	_, err := store.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing server")
	}
}

func TestRemoveNotFound(t *testing.T) {
	store, _ := Load(filepath.Join(t.TempDir(), "mcpservers.yaml"))
	err := store.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for missing server")
	}
}

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcpservers.yaml")
	store, _ := Load(path)

	store.Add("test", McpServerDef{URL: "https://test.example.com", Secret: "test-key"})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	def, err := reloaded.Get("test")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if def.URL != "https://test.example.com" {
		t.Errorf("URL = %q, want test URL", def.URL)
	}
	if def.Secret != "test-key" {
		t.Errorf("Secret = %q, want test-key", def.Secret)
	}
}

func TestAll(t *testing.T) {
	store, _ := Load(filepath.Join(t.TempDir(), "mcpservers.yaml"))

	store.Add("a", McpServerDef{URL: "https://a.example.com"})
	store.Add("b", McpServerDef{URL: "https://b.example.com", Secret: "b-key"})

	all := store.All()
	if len(all) != 2 {
		t.Fatalf("All = %v, want 2 entries", all)
	}
	if all["a"].URL != "https://a.example.com" {
		t.Errorf("a.URL = %q", all["a"].URL)
	}
	if all["b"].Secret != "b-key" {
		t.Errorf("b.Secret = %q", all["b"].Secret)
	}
}
