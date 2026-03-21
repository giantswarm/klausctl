package oauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	serverURL := "https://muster.example.com/mcp"
	issuerURL := "https://dex.example.com"
	token := Token{
		AccessToken:  "test-access-token",
		TokenType:    "Bearer",
		RefreshToken: "test-refresh-token",
		ExpiresIn:    3600,
	}

	if err := store.StoreToken(serverURL, issuerURL, token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	got := store.GetToken(serverURL)
	if got == nil {
		t.Fatal("GetToken returned nil")
	}
	if got.Token.AccessToken != "test-access-token" {
		t.Errorf("AccessToken = %q, want %q", got.Token.AccessToken, "test-access-token")
	}
	if got.Token.RefreshToken != "test-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", got.Token.RefreshToken, "test-refresh-token")
	}
	if got.Issuer != issuerURL {
		t.Errorf("Issuer = %q, want %q", got.Issuer, issuerURL)
	}
	if got.ServerURL != serverURL {
		t.Errorf("ServerURL = %q, want %q", got.ServerURL, serverURL)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestTokenStoreFilePermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	serverURL := "https://muster.example.com/mcp"
	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{AccessToken: "test"}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestTokenStoreGetMissing(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	if got := store.GetToken("https://nonexistent.example.com"); got != nil {
		t.Errorf("expected nil for missing token, got %+v", got)
	}
}

func TestTokenStoreDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	serverURL := "https://muster.example.com/mcp"
	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{AccessToken: "test"}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	if err := store.DeleteToken(serverURL); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	if got := store.GetToken(serverURL); got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestTokenStoreDeleteMissing(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	if err := store.DeleteToken("https://nonexistent.example.com"); err != nil {
		t.Errorf("unexpected error deleting nonexistent token: %v", err)
	}
}

func TestTokenStoreGetValidToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	serverURL := "https://muster.example.com/mcp"

	// Store a token that expires in 1 hour.
	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{
		AccessToken: "valid-token",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	if got := store.GetValidToken(serverURL); got == nil {
		t.Fatal("expected valid token, got nil")
	}

	if !store.HasValidToken(serverURL) {
		t.Error("HasValidToken returned false for valid token")
	}
}

func TestTokenStoreExpiredToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	serverURL := "https://muster.example.com/mcp"

	origNow := nowFunc
	nowFunc = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	defer func() { nowFunc = origNow }()

	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{
		AccessToken: "expired-token",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	nowFunc = origNow

	if got := store.GetValidToken(serverURL); got != nil {
		t.Errorf("expected nil for expired token, got %+v", got)
	}

	if store.HasValidToken(serverURL) {
		t.Error("HasValidToken returned true for expired token")
	}

	// Raw GetToken should still return it.
	if got := store.GetToken(serverURL); got == nil {
		t.Error("GetToken returned nil for expired token (should still return it)")
	}
}

func TestTokenStoreListTokens(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	if err := store.StoreToken("https://b.example.com", "https://dex-b.example.com", Token{
		AccessToken: "token-b",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreToken("https://a.example.com", "https://dex-a.example.com", Token{
		AccessToken: "token-a",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	statuses, err := store.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses[0].ServerURL != "https://a.example.com" {
		t.Errorf("first status server = %q, want a.example.com", statuses[0].ServerURL)
	}
	if statuses[1].ServerURL != "https://b.example.com" {
		t.Errorf("second status server = %q, want b.example.com", statuses[1].ServerURL)
	}
	if statuses[0].Status != "valid" {
		t.Errorf("first status = %q, want valid", statuses[0].Status)
	}
}

func TestTokenStoreListEmpty(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	statuses, err := store.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected empty list, got %d entries", len(statuses))
	}
}

func TestTokenStoreListNonexistentDir(t *testing.T) {
	store := NewTokenStore(filepath.Join(t.TempDir(), "nonexistent"))
	statuses, err := store.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if statuses != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", statuses)
	}
}

func TestTokenStoreDirectoryPermissions(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "tokens")
	store := NewTokenStore(dir)

	if err := store.StoreToken("https://test.example.com", "https://dex.example.com", Token{AccessToken: "test"}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o700 {
		t.Errorf("directory permissions = %04o, want 0700", perm)
	}
}
