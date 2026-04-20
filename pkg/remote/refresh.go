package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrReloginRequired is returned when the stored refresh token is missing
// or the OAuth server rejects it. Callers surface this to the user as a
// prompt to re-run `klausctl auth login --remote=...`.
type ErrReloginRequired struct {
	ServerURL string
	Reason    string
}

func (e *ErrReloginRequired) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("re-authentication required for %s", e.ServerURL)
	}
	return fmt.Sprintf("re-authentication required for %s: %s", e.ServerURL, e.Reason)
}

// refreshResponse is the subset of an RFC 6749 token endpoint reply that
// we care about for the refresh grant.
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// Refresh exchanges the refresh token stored in rec for a new access
// token via the cached token endpoint. The updated record (same fields,
// new AccessToken/ExpiresAt/RefreshToken) is returned; callers are
// responsible for persisting it with AuthStore.Put.
//
// If the server rejects the refresh (401/invalid_grant) Refresh returns
// *ErrReloginRequired so the caller can surface a "re-run auth login"
// hint to the user.
func Refresh(ctx context.Context, httpClient *http.Client, rec AuthRecord) (AuthRecord, error) {
	if rec.RefreshToken == "" {
		return rec, &ErrReloginRequired{ServerURL: rec.ServerURL, Reason: "no refresh token stored"}
	}
	if rec.TokenEndpoint == "" {
		return rec, &ErrReloginRequired{ServerURL: rec.ServerURL, Reason: "no token endpoint cached"}
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rec.RefreshToken},
	}
	if rec.ClientID != "" {
		form.Set("client_id", rec.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rec.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return rec, fmt.Errorf("building refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return rec, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
		return rec, &ErrReloginRequired{
			ServerURL: rec.ServerURL,
			Reason:    fmt.Sprintf("refresh rejected (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return rec, fmt.Errorf("refresh returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload refreshResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return rec, fmt.Errorf("parsing refresh response: %w", err)
	}
	if payload.AccessToken == "" {
		return rec, fmt.Errorf("refresh response missing access_token")
	}

	updated := rec
	updated.AccessToken = payload.AccessToken
	if payload.TokenType != "" {
		updated.TokenType = payload.TokenType
	}
	if payload.RefreshToken != "" {
		updated.RefreshToken = payload.RefreshToken
	}
	if payload.ExpiresIn > 0 {
		updated.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	} else {
		updated.ExpiresAt = time.Time{}
	}
	if payload.Scope != "" {
		updated.Scope = payload.Scope
	}
	return updated, nil
}
