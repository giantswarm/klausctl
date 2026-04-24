package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestRefreshSuccess(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		parsed, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form body: %v", err)
		}
		gotForm = parsed
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"token_type":    "Bearer",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
			"scope":         "openid profile",
		})
	}))
	defer srv.Close()

	rec := AuthRecord{
		ServerURL:     "https://gw.example.com",
		RefreshToken:  "old-refresh",
		TokenEndpoint: srv.URL,
		ClientID:      "cid-123",
	}
	updated, err := Refresh(context.Background(), srv.Client(), rec)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if updated.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want new-access", updated.AccessToken)
	}
	if updated.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q, want new-refresh", updated.RefreshToken)
	}
	if updated.Scope != "openid profile" {
		t.Errorf("Scope = %q, want openid profile", updated.Scope)
	}
	if updated.ExpiresAt.IsZero() || time.Until(updated.ExpiresAt) < 30*time.Minute {
		t.Errorf("ExpiresAt not set to expected future time: %v", updated.ExpiresAt)
	}

	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
	}
	if gotForm.Get("refresh_token") != "old-refresh" {
		t.Errorf("refresh_token form field = %q, want old-refresh", gotForm.Get("refresh_token"))
	}
	if gotForm.Get("client_id") != "cid-123" {
		t.Errorf("client_id form field = %q, want cid-123", gotForm.Get("client_id"))
	}
}

func TestRefreshNoRefreshToken(t *testing.T) {
	rec := AuthRecord{ // #nosec G101 -- constant identifier, not a credential
		ServerURL:     "https://gw.example.com",
		TokenEndpoint: "https://auth.example.com/token",
	}
	_, err := Refresh(context.Background(), http.DefaultClient, rec)
	var relogin *ErrReloginRequired
	if !errors.As(err, &relogin) {
		t.Fatalf("expected *ErrReloginRequired, got %v (%T)", err, err)
	}
}

func TestRefreshNoTokenEndpoint(t *testing.T) {
	rec := AuthRecord{
		ServerURL:    "https://gw.example.com",
		RefreshToken: "rt",
	}
	_, err := Refresh(context.Background(), http.DefaultClient, rec)
	var relogin *ErrReloginRequired
	if !errors.As(err, &relogin) {
		t.Fatalf("expected *ErrReloginRequired, got %v (%T)", err, err)
	}
}

func TestRefresh401YieldsReloginRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	rec := AuthRecord{
		ServerURL:     "https://gw.example.com",
		RefreshToken:  "rt",
		TokenEndpoint: srv.URL,
	}
	_, err := Refresh(context.Background(), srv.Client(), rec)
	var relogin *ErrReloginRequired
	if !errors.As(err, &relogin) {
		t.Fatalf("expected *ErrReloginRequired on 401, got %v (%T)", err, err)
	}
}

func TestRefresh400YieldsReloginRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	rec := AuthRecord{
		ServerURL:     "https://gw.example.com",
		RefreshToken:  "rt",
		TokenEndpoint: srv.URL,
	}
	_, err := Refresh(context.Background(), srv.Client(), rec)
	var relogin *ErrReloginRequired
	if !errors.As(err, &relogin) {
		t.Fatalf("expected *ErrReloginRequired on 400, got %v (%T)", err, err)
	}
}

func TestRefresh500ReturnsGenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	rec := AuthRecord{
		ServerURL:     "https://gw.example.com",
		RefreshToken:  "rt",
		TokenEndpoint: srv.URL,
	}
	_, err := Refresh(context.Background(), srv.Client(), rec)
	if err == nil {
		t.Fatal("expected error on 500")
	}
	var relogin *ErrReloginRequired
	if errors.As(err, &relogin) {
		t.Errorf("5xx should not be classified as relogin: %v", err)
	}
}
