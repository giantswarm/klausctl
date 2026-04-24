package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/giantswarm/klausctl/pkg/oauth"
)

// LoginOptions controls Login behaviour. Fields left at zero fall back
// to sensible defaults (CIMD URL, default scopes, http.DefaultClient).
type LoginOptions struct {
	// ClientID is the OAuth client identifier; defaults to the public
	// CIMD URL (oauth.DefaultClientIDMetadataURL).
	ClientID string
	// Scopes is the space-separated scope string to request. Defaults
	// to "openid profile email groups offline_access".
	Scopes string
	// HTTPClient is used for the token exchange; defaults to
	// http.DefaultClient when nil.
	HTTPClient *http.Client
	// BrowserOpener opens the authorize URL in the user's browser.
	// Defaults to oauth.OpenBrowser.
	BrowserOpener func(string) error
}

const defaultScopes = "openid profile email groups offline_access"

// Login performs browser-based OAuth login against a remote klaus-gateway
// and persists the resulting credentials in the given AuthStore. It
// reuses the pkg/oauth helpers for discovery/PKCE/callback and implements
// the authorization-URL build + token exchange locally so the resulting
// AuthRecord captures the token endpoint and client id needed for later
// refresh-on-401 in pkg/remote/refresh.go.
func Login(ctx context.Context, store *AuthStore, serverURL string, opts LoginOptions) (*AuthRecord, error) {
	normURL, err := NormalizeBaseURL(serverURL)
	if err != nil {
		return nil, err
	}

	if opts.ClientID == "" {
		opts.ClientID = oauth.DefaultClientIDMetadataURL
	}
	if opts.Scopes == "" {
		opts.Scopes = defaultScopes
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.BrowserOpener == nil {
		opts.BrowserOpener = oauth.OpenBrowser
	}

	issuer, err := discoverIssuer(ctx, normURL)
	if err != nil {
		return nil, fmt.Errorf("discovering issuer for %s: %w", normURL, err)
	}

	meta, err := oauth.DiscoverMetadata(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OAuth metadata: %w", err)
	}

	pkce := oauth.GeneratePKCE()
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	cb := oauth.NewCallbackServer(state)
	redirectURI, err := cb.Start()
	if err != nil {
		return nil, fmt.Errorf("starting callback server: %w", err)
	}

	authURL := buildAuthorizeURL(meta.AuthorizationEndpoint, opts.ClientID, redirectURI, state, opts.Scopes, pkce)

	fmt.Printf("Opening browser for authentication against %s...\n", issuer)
	fmt.Printf("If the browser does not open, visit:\n  %s\n\n", authURL)

	if err := opts.BrowserOpener(authURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}

	result, err := cb.WaitForCallback(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for callback: %w", err)
	}

	tok, err := exchangeAuthorizationCode(ctx, opts.HTTPClient, meta.TokenEndpoint, opts.ClientID, redirectURI, result.Code, pkce.Verifier)
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}

	rec := AuthRecord{
		ServerURL:     normURL,
		Issuer:        issuer,
		AccessToken:   tok.AccessToken,
		TokenType:     tok.TokenType,
		RefreshToken:  tok.RefreshToken,
		Scope:         tok.Scope,
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      opts.ClientID,
	}
	if tok.ExpiresIn > 0 {
		rec.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}

	if err := store.Put(rec); err != nil {
		return nil, fmt.Errorf("storing credentials: %w", err)
	}
	return &rec, nil
}

// discoverIssuer resolves the issuer URL for a klaus-gateway by probing
// its MCP endpoint. Klaus-gateway exposes OAuth-protected resource
// metadata at `<base>/v1/mcp` (or returns 401 with WWW-Authenticate).
func discoverIssuer(ctx context.Context, baseURL string) (string, error) {
	probeURL := baseURL + "/mcp"

	challenge, err := oauth.ProbeServer(ctx, probeURL)
	if err != nil {
		return "", fmt.Errorf("probing %s: %w", probeURL, err)
	}
	if challenge == nil {
		return "", fmt.Errorf("%s does not require OAuth authentication", probeURL)
	}

	if challenge.Realm != "" {
		return challenge.Realm, nil
	}
	if challenge.ResourceMetadata == "" {
		return "", fmt.Errorf("WWW-Authenticate at %s contained no realm or resource_metadata", probeURL)
	}
	resMeta, err := oauth.FetchResourceMetadata(ctx, challenge.ResourceMetadata)
	if err != nil {
		return "", fmt.Errorf("fetching resource metadata: %w", err)
	}
	if len(resMeta.AuthorizationServers) == 0 {
		return "", fmt.Errorf("resource metadata at %s has no authorization_servers", challenge.ResourceMetadata)
	}
	return resMeta.AuthorizationServers[0], nil
}

func buildAuthorizeURL(endpoint, clientID, redirectURI, state, scopes string, pkce oauth.PKCEChallenge) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {scopes},
		"state":                 {state},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.ChallengeMethod},
	}
	return endpoint + "?" + params.Encode()
}

// tokenEndpointResponse captures the subset of RFC 6749 response fields
// we persist.
type tokenEndpointResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func exchangeAuthorizationCode(ctx context.Context, httpClient *http.Client, tokenEndpoint, clientID, redirectURI, code, verifier string) (*tokenEndpointResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok tokenEndpointResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &tok, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
