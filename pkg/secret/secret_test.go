package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "secrets.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names := store.List(); len(names) != 0 {
		t.Errorf("expected empty list, got %v", names)
	}
}

func TestSetGetDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	store.Set("api-key", "sk-123")
	store.Set("db-pass", "hunter2")

	val, err := store.Get("api-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "sk-123" {
		t.Errorf("Get = %q, want %q", val, "sk-123")
	}

	names := store.List()
	if len(names) != 2 {
		t.Fatalf("List = %v, want 2 entries", names)
	}
	if names[0] != "api-key" || names[1] != "db-pass" {
		t.Errorf("List = %v, want [api-key db-pass]", names)
	}

	if err := store.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get("api-key"); err == nil {
		t.Error("expected error for deleted key")
	}
}

func TestGetNotFound(t *testing.T) {
	store, _ := Load(filepath.Join(t.TempDir(), "secrets.yaml"))
	_, err := store.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestDeleteNotFound(t *testing.T) {
	store, _ := Load(filepath.Join(t.TempDir(), "secrets.yaml"))
	err := store.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	store, _ := Load(path)

	store.Set("token", "abc123")
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %04o, want 0600", perm)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	val, err := reloaded.Get("token")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if val != "abc123" {
		t.Errorf("value = %q, want %q", val, "abc123")
	}
}

func TestLoadBadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte("key: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for world-readable secrets file")
	}
}
