package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeServer_401WithWWWAuthenticate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("WWW-Authenticate", `Bearer realm="https://dex.example.com"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}))
	defer server.Close()

	challenge, err := ProbeServer(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProbeServer: %v", err)
	}
	if challenge == nil {
		t.Fatal("expected non-nil challenge")
	}
	if challenge.Realm != "https://dex.example.com" {
		t.Errorf("Realm = %q, want https://dex.example.com", challenge.Realm)
	}
}

func TestProbeServer_401WithResourceMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("WWW-Authenticate", `Bearer realm="https://dex.example.com", resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}))
	defer server.Close()

	challenge, err := ProbeServer(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProbeServer: %v", err)
	}
	if challenge == nil {
		t.Fatal("expected non-nil challenge")
	}
	if challenge.Realm != "https://dex.example.com" {
		t.Errorf("Realm = %q", challenge.Realm)
	}
	if challenge.ResourceMetadata != "https://mcp.example.com/.well-known/oauth-protected-resource" {
		t.Errorf("ResourceMetadata = %q", challenge.ResourceMetadata)
	}
}

func TestProbeServer_200NoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	challenge, err := ProbeServer(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProbeServer: %v", err)
	}
	if challenge != nil {
		t.Errorf("expected nil challenge for 200 response, got %+v", challenge)
	}
}

func TestProbeServer_401NoHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	challenge, err := ProbeServer(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProbeServer: %v", err)
	}
	if challenge != nil {
		t.Errorf("expected nil challenge when no WWW-Authenticate, got %+v", challenge)
	}
}

func TestProbeServer_FallbackResourceMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"resource": "test"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	challenge, err := ProbeServer(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProbeServer: %v", err)
	}
	if challenge == nil {
		t.Fatal("expected non-nil challenge from resource metadata fallback")
	}
	if challenge.ResourceMetadata == "" {
		t.Error("expected ResourceMetadata to be set")
	}
}
