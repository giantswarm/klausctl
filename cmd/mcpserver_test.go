package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
	"github.com/giantswarm/klausctl/pkg/oauth"
)

func TestAuthLabel_Secret(t *testing.T) {
	store := oauth.NewTokenStore(t.TempDir())
	def := mcpserverstore.McpServerDef{
		URL:    "https://muster.example.com/mcp",
		Secret: "my-secret",
	}

	got := authLabel(def, store)
	if got != "secret" { //nolint:goconst
		t.Errorf("authLabel = %q, want secret", got)
	}
}

func TestAuthLabel_NoAuth(t *testing.T) {
	store := oauth.NewTokenStore(t.TempDir())
	def := mcpserverstore.McpServerDef{
		URL: "https://muster.example.com/mcp",
	}

	got := authLabel(def, store)
	if got != "-" {
		t.Errorf("authLabel = %q, want -", got)
	}
}

func TestAuthLabel_ValidOAuth(t *testing.T) {
	dir := t.TempDir()
	store := oauth.NewTokenStore(dir)
	serverURL := "https://muster.example.com/mcp" //nolint:goconst

	if err := store.StoreToken(serverURL, "https://dex.example.com", oauth.Token{
		AccessToken: "valid-token",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	def := mcpserverstore.McpServerDef{URL: serverURL}
	got := authLabel(def, store)
	if got != "oauth" {
		t.Errorf("authLabel = %q, want oauth", got)
	}
}

func TestAuthLabel_ExpiredOAuth(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	serverURL := "https://muster.example.com/mcp"
	writeExpiredToken(t, dir, serverURL, "https://dex.example.com")

	store := oauth.NewTokenStore(dir)
	def := mcpserverstore.McpServerDef{URL: serverURL}
	got := authLabel(def, store)
	if got != "oauth (expired)" {
		t.Errorf("authLabel = %q, want oauth (expired)", got)
	}
}

func writeExpiredToken(t *testing.T, dir, serverURL, issuer string) {
	t.Helper()
	type storedToken struct {
		Token     oauth.Token `json:"token"`
		Issuer    string      `json:"issuer"`
		ServerURL string      `json:"server_url"`
		CreatedAt time.Time   `json:"created_at"`
	}
	st := storedToken{
		Token:     oauth.Token{AccessToken: "expired-token", ExpiresIn: 3600},
		Issuer:    issuer,
		ServerURL: serverURL,
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(serverURL))
	path := filepath.Join(dir, hex.EncodeToString(h[:])+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuthLabel_SecretTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	store := oauth.NewTokenStore(dir)
	serverURL := "https://muster.example.com/mcp"

	if err := store.StoreToken(serverURL, "https://dex.example.com", oauth.Token{
		AccessToken: "valid-token",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	def := mcpserverstore.McpServerDef{
		URL:    serverURL,
		Secret: "my-secret",
	}
	got := authLabel(def, store)
	if got != "secret" {
		t.Errorf("authLabel = %q, want secret (secret should take precedence)", got)
	}
}
