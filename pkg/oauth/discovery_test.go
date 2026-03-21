package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverMetadata_RFC8414(t *testing.T) {
	ClearMetadataCache()

	meta := Metadata{
		Issuer:                "https://dex.example.com",
		AuthorizationEndpoint: "https://dex.example.com/auth",
		TokenEndpoint:         "https://dex.example.com/token",
		ScopesSupported:       []string{"openid", "profile"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(meta)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	got, err := DiscoverMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DiscoverMetadata: %v", err)
	}
	if got.AuthorizationEndpoint != meta.AuthorizationEndpoint {
		t.Errorf("AuthorizationEndpoint = %q, want %q", got.AuthorizationEndpoint, meta.AuthorizationEndpoint)
	}
	if got.TokenEndpoint != meta.TokenEndpoint {
		t.Errorf("TokenEndpoint = %q, want %q", got.TokenEndpoint, meta.TokenEndpoint)
	}
}

func TestDiscoverMetadata_OpenIDConnect(t *testing.T) {
	ClearMetadataCache()

	meta := Metadata{
		Issuer:                "https://dex.example.com",
		AuthorizationEndpoint: "https://dex.example.com/auth",
		TokenEndpoint:         "https://dex.example.com/token",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(meta)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	got, err := DiscoverMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DiscoverMetadata: %v", err)
	}
	if got.TokenEndpoint != meta.TokenEndpoint {
		t.Errorf("TokenEndpoint = %q, want %q", got.TokenEndpoint, meta.TokenEndpoint)
	}
}

func TestDiscoverMetadata_PrefersRFC8414(t *testing.T) {
	ClearMetadataCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(Metadata{
				Issuer:                "rfc8414",
				AuthorizationEndpoint: "https://dex.example.com/auth-rfc",
				TokenEndpoint:         "https://dex.example.com/token-rfc",
			})
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(Metadata{
				Issuer:                "oidc",
				AuthorizationEndpoint: "https://dex.example.com/auth-oidc",
				TokenEndpoint:         "https://dex.example.com/token-oidc",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	got, err := DiscoverMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DiscoverMetadata: %v", err)
	}
	if got.Issuer != "rfc8414" {
		t.Errorf("expected RFC 8414 result, got issuer %q", got.Issuer)
	}
}

func TestDiscoverMetadata_MissingEndpoints(t *testing.T) {
	ClearMetadataCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Metadata{Issuer: "https://dex.example.com"})
	}))
	defer server.Close()

	_, err := DiscoverMetadata(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for metadata missing required endpoints")
	}
}

func TestDiscoverMetadata_NotFound(t *testing.T) {
	ClearMetadataCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := DiscoverMetadata(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error when no discovery endpoint responds")
	}
}

func TestDiscoverMetadata_CachesResults(t *testing.T) {
	ClearMetadataCache()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Metadata{
			Issuer:                "https://dex.example.com",
			AuthorizationEndpoint: "https://dex.example.com/auth",
			TokenEndpoint:         "https://dex.example.com/token",
		})
	}))
	defer server.Close()

	_, err := DiscoverMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err = DiscoverMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP request (cached), got %d", callCount)
	}
}

func TestDiscoverMetadata_TrailingSlash(t *testing.T) {
	ClearMetadataCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Metadata{
				Issuer:                "https://dex.example.com",
				AuthorizationEndpoint: "https://dex.example.com/auth",
				TokenEndpoint:         "https://dex.example.com/token",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	got, err := DiscoverMetadata(context.Background(), server.URL+"/")
	if err != nil {
		t.Fatalf("DiscoverMetadata with trailing slash: %v", err)
	}
	if got.AuthorizationEndpoint != "https://dex.example.com/auth" {
		t.Errorf("AuthorizationEndpoint = %q", got.AuthorizationEndpoint)
	}
}

func TestDiscoverMetadata_InvalidJSON(t *testing.T) {
	ClearMetadataCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := DiscoverMetadata(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClearMetadataCache(t *testing.T) {
	metadataCacheMu.Lock()
	metadataCache["test"] = cachedMetadata{}
	metadataCacheMu.Unlock()

	ClearMetadataCache()

	metadataCacheMu.Lock()
	n := len(metadataCache)
	metadataCacheMu.Unlock()

	if n != 0 {
		t.Errorf("expected empty cache, got %d entries", n)
	}
}
