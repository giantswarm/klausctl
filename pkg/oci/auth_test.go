package oci

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestCredentialFromJSON(t *testing.T) {
	// Create a Docker config JSON with credentials.
	authValue := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	configJSON := `{"auths":{"registry.example.com":{"auth":"` + authValue + `"}}}`

	cred, ok := credentialFromJSON([]byte(configJSON), "registry.example.com")
	if !ok {
		t.Fatal("credentialFromJSON() returned false")
	}
	if cred.Username != "user" {
		t.Errorf("Username = %q, want %q", cred.Username, "user")
	}
	if cred.Password != "pass" {
		t.Errorf("Password = %q, want %q", cred.Password, "pass")
	}
}

func TestCredentialFromJSONWithPort(t *testing.T) {
	authValue := base64.StdEncoding.EncodeToString([]byte("user:secret"))
	configJSON := `{"auths":{"registry.example.com":{"auth":"` + authValue + `"}}}`

	// Lookup with port should fall back to host-only match.
	cred, ok := credentialFromJSON([]byte(configJSON), "registry.example.com:443")
	if !ok {
		t.Fatal("credentialFromJSON() should match host without port")
	}
	if cred.Username != "user" {
		t.Errorf("Username = %q, want %q", cred.Username, "user")
	}
}

func TestCredentialFromJSONNoMatch(t *testing.T) {
	authValue := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	configJSON := `{"auths":{"other.example.com":{"auth":"` + authValue + `"}}}`

	_, ok := credentialFromJSON([]byte(configJSON), "registry.example.com")
	if ok {
		t.Error("credentialFromJSON() should return false for non-matching host")
	}
}

func TestCredentialFromJSONInvalidJSON(t *testing.T) {
	_, ok := credentialFromJSON([]byte("not json"), "registry.example.com")
	if ok {
		t.Error("credentialFromJSON() should return false for invalid JSON")
	}
}

func TestCredentialFromJSONInvalidAuth(t *testing.T) {
	configJSON := `{"auths":{"registry.example.com":{"auth":"not-valid-base64!!!"}}}`

	_, ok := credentialFromJSON([]byte(configJSON), "registry.example.com")
	if ok {
		t.Error("credentialFromJSON() should return false for invalid base64 auth")
	}
}

func TestCredentialFromFile(t *testing.T) {
	dir := t.TempDir()
	authValue := base64.StdEncoding.EncodeToString([]byte("fileuser:filepass"))
	configJSON := `{"auths":{"gsoci.azurecr.io":{"auth":"` + authValue + `"}}}`

	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(configJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cred, ok := credentialFromFile(path, "gsoci.azurecr.io")
	if !ok {
		t.Fatal("credentialFromFile() returned false")
	}
	if cred.Username != "fileuser" {
		t.Errorf("Username = %q, want %q", cred.Username, "fileuser")
	}
}

func TestCredentialFromFileMissing(t *testing.T) {
	_, ok := credentialFromFile("/nonexistent/config.json", "registry.example.com")
	if ok {
		t.Error("credentialFromFile() should return false for missing file")
	}
}

func TestCredentialFromEnv(t *testing.T) {
	authValue := base64.StdEncoding.EncodeToString([]byte("envuser:envpass"))
	configJSON := `{"auths":{"registry.example.com":{"auth":"` + authValue + `"}}}`
	envValue := base64.StdEncoding.EncodeToString([]byte(configJSON))

	cred, ok := credentialFromEnv(envValue, "registry.example.com")
	if !ok {
		t.Fatal("credentialFromEnv() returned false")
	}
	if cred.Username != "envuser" {
		t.Errorf("Username = %q, want %q", cred.Username, "envuser")
	}
}

func TestCredentialFromEnvInvalid(t *testing.T) {
	_, ok := credentialFromEnv("not-valid-base64!!!", "registry.example.com")
	if ok {
		t.Error("credentialFromEnv() should return false for invalid base64")
	}
}

func TestNewAuthClient(t *testing.T) {
	client := newAuthClient()
	if client == nil {
		t.Fatal("newAuthClient() returned nil")
	}
	if client.Client == nil {
		t.Error("auth client HTTP client should not be nil")
	}
	if client.Credential == nil {
		t.Error("auth client Credential func should not be nil")
	}
}

func TestCredentialFromJSONPasswordWithColon(t *testing.T) {
	// Passwords can contain colons.
	authValue := base64.StdEncoding.EncodeToString([]byte("user:pass:with:colons"))
	configJSON := `{"auths":{"registry.example.com":{"auth":"` + authValue + `"}}}`

	cred, ok := credentialFromJSON([]byte(configJSON), "registry.example.com")
	if !ok {
		t.Fatal("credentialFromJSON() returned false")
	}
	if cred.Username != "user" {
		t.Errorf("Username = %q, want %q", cred.Username, "user")
	}
	if cred.Password != "pass:with:colons" {
		t.Errorf("Password = %q, want %q", cred.Password, "pass:with:colons")
	}
}

func TestResolveCredentialAnonymousFallback(t *testing.T) {
	// Unset the env var to ensure fallback to anonymous.
	t.Setenv("KLAUSCTL_REGISTRY_AUTH", "")

	cred, err := resolveCredential(nil, "unknown-registry.example.com")
	if err != nil {
		t.Fatalf("resolveCredential() error = %v", err)
	}
	if cred != auth.EmptyCredential {
		t.Errorf("expected anonymous credential, got %+v", cred)
	}
}
