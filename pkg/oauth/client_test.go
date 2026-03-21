package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthURL(t *testing.T) {
	pkce := PKCEChallenge{
		Verifier:        "test-verifier",
		Challenge:       "test-challenge",
		ChallengeMethod: "S256",
	}

	result := buildAuthURL("https://dex.example.com/auth", DefaultClientIDMetadataURL, "http://127.0.0.1:3001/callback", "test-state", pkce)

	u, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	if u.Scheme != "https" || u.Host != "dex.example.com" || u.Path != "/auth" {
		t.Errorf("unexpected base URL: %s", result)
	}

	params := u.Query()
	checks := map[string]string{
		"response_type":         "code",
		"client_id":             DefaultClientIDMetadataURL,
		"redirect_uri":          "http://127.0.0.1:3001/callback",
		"state":                 "test-state",
		"code_challenge":        "test-challenge",
		"code_challenge_method": "S256",
	}
	for key, want := range checks {
		if got := params.Get(key); got != want {
			t.Errorf("param %q = %q, want %q", key, got, want)
		}
	}

	scope := params.Get("scope")
	for _, s := range []string{"openid", "profile", "email", "groups", "offline_access"} {
		if !strings.Contains(scope, s) {
			t.Errorf("scope missing %q, got %q", s, scope)
		}
	}
}

func TestExchangeCode_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}

		r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "test-code" {
			t.Errorf("code = %q", r.Form.Get("code"))
		}
		if r.Form.Get("code_verifier") != "test-verifier" {
			t.Errorf("code_verifier = %q", r.Form.Get("code_verifier"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Token{
			AccessToken:  "access-token-xyz",
			TokenType:    "Bearer",
			RefreshToken: "refresh-token-xyz",
			ExpiresIn:    3600,
		})
	}))
	defer server.Close()

	token, err := exchangeCode(context.Background(), server.URL, "klausctl", "http://localhost/cb", "test-code", "test-verifier")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if token.AccessToken != "access-token-xyz" {
		t.Errorf("AccessToken = %q", token.AccessToken)
	}
	if token.RefreshToken != "refresh-token-xyz" {
		t.Errorf("RefreshToken = %q", token.RefreshToken)
	}
	if token.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d", token.ExpiresIn)
	}
}

func TestExchangeCode_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	_, err := exchangeCode(context.Background(), server.URL, "klausctl", "http://localhost/cb", "bad-code", "verifier")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestExchangeCode_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Token{TokenType: "Bearer"})
	}))
	defer server.Close()

	_, err := exchangeCode(context.Background(), server.URL, "klausctl", "http://localhost/cb", "code", "verifier")
	if err == nil {
		t.Fatal("expected error for missing access_token")
	}
}

func TestExchangeCode_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := exchangeCode(context.Background(), server.URL, "klausctl", "http://localhost/cb", "code", "verifier")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClientAuthStatus_NoToken(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	client := NewClient(store)

	status := client.AuthStatus("https://muster.example.com/mcp")
	if status.Status != "none" {
		t.Errorf("Status = %q, want none", status.Status)
	}
	if status.ServerURL != "https://muster.example.com/mcp" {
		t.Errorf("ServerURL = %q", status.ServerURL)
	}
}

func TestClientAuthStatus_ValidToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)
	serverURL := "https://muster.example.com/mcp"

	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{
		AccessToken: "valid",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	client := NewClient(store)
	status := client.AuthStatus(serverURL)
	if status.Status != "valid" {
		t.Errorf("Status = %q, want valid", status.Status)
	}
	if status.Issuer != "https://dex.example.com" {
		t.Errorf("Issuer = %q", status.Issuer)
	}
}

func TestClientAuthStatus_ExpiredToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)
	serverURL := "https://muster.example.com/mcp"

	origNow := nowFunc
	nowFunc = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{
		AccessToken: "expired",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}
	nowFunc = origNow
	defer func() { nowFunc = origNow }()

	client := NewClient(store)
	status := client.AuthStatus(serverURL)
	if status.Status != "expired" {
		t.Errorf("Status = %q, want expired", status.Status)
	}
}

func TestClientLogout(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)
	serverURL := "https://muster.example.com/mcp"

	if err := store.StoreToken(serverURL, "https://dex.example.com", Token{AccessToken: "test"}); err != nil {
		t.Fatal(err)
	}

	client := NewClient(store)
	if err := client.Logout(serverURL); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if store.GetToken(serverURL) != nil {
		t.Error("token should be deleted after logout")
	}
}

func TestClientLogout_NoToken(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	client := NewClient(store)

	if err := client.Logout("https://nonexistent.example.com"); err != nil {
		t.Errorf("Logout nonexistent: %v", err)
	}
}

func TestGenerateState(t *testing.T) {
	a, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if len(a) != 32 {
		t.Errorf("state length = %d, want 32 hex chars", len(a))
	}

	b, _ := generateState()
	if a == b {
		t.Error("two consecutive states should differ")
	}
}
