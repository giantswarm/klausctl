package oauth

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
)

const (
	// DefaultClientIDMetadataURL is the CIMD URL hosted on GitHub Pages.
	// Authorization servers that support Client ID Metadata Documents (CIMD)
	// will fetch this URL to validate client metadata (redirect URIs, etc.).
	DefaultClientIDMetadataURL = "https://giantswarm.github.io/klausctl/client.json"

	defaultScopes = "openid profile email groups offline_access"
)

// Client orchestrates the full OAuth authorization code flow with PKCE.
type Client struct {
	store *TokenStore
}

// NewClient creates a Client backed by the given TokenStore.
func NewClient(store *TokenStore) *Client {
	return &Client{store: store}
}

// Login performs browser-based OAuth login for the given MCP server URL.
// It probes the server, discovers OAuth metadata, starts a local callback
// server, opens the browser for user authentication, exchanges the
// authorization code, and stores the resulting token.
func (c *Client) Login(ctx context.Context, serverURL string) error {
	challenge, err := ProbeServer(ctx, serverURL)
	if err != nil {
		return fmt.Errorf("probing server: %w", err)
	}
	if challenge == nil {
		return fmt.Errorf("server %s does not require OAuth authentication", serverURL)
	}

	issuerURL := challenge.Realm
	if issuerURL == "" && challenge.ResourceMetadata != "" {
		resMeta, rmErr := FetchResourceMetadata(ctx, challenge.ResourceMetadata)
		if rmErr != nil {
			return fmt.Errorf("fetching resource metadata: %w", rmErr)
		}
		if len(resMeta.AuthorizationServers) == 0 {
			return fmt.Errorf("resource metadata at %s has no authorization_servers", challenge.ResourceMetadata)
		}
		issuerURL = resMeta.AuthorizationServers[0]
	}
	if issuerURL == "" {
		return fmt.Errorf("server %s returned no issuer URL in WWW-Authenticate", serverURL)
	}

	meta, err := DiscoverMetadata(ctx, issuerURL)
	if err != nil {
		return fmt.Errorf("discovering OAuth metadata: %w", err)
	}

	pkce := GeneratePKCE()
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("generating state parameter: %w", err)
	}

	callbackServer := NewCallbackServer(state)
	redirectURI, err := callbackServer.Start()
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}

	authURL := buildAuthURL(meta.AuthorizationEndpoint, DefaultClientIDMetadataURL, redirectURI, state, pkce)

	fmt.Printf("Opening browser for authentication...\n")
	fmt.Printf("If the browser does not open, visit:\n  %s\n\n", authURL)

	if err := OpenBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}

	result, err := callbackServer.WaitForCallback(ctx)
	if err != nil {
		return fmt.Errorf("waiting for callback: %w", err)
	}

	token, err := exchangeCode(ctx, meta.TokenEndpoint, DefaultClientIDMetadataURL, redirectURI, result.Code, pkce.Verifier)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}

	if err := c.store.StoreToken(serverURL, issuerURL, *token); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	return nil
}

// AuthStatus returns the token status for the given server URL.
func (c *Client) AuthStatus(serverURL string) TokenStatus {
	st := c.store.GetToken(serverURL)
	if st == nil {
		return TokenStatus{
			ServerURL: serverURL,
			Status:    "none",
		}
	}

	status := TokenStatus{
		ServerURL: st.ServerURL,
		Issuer:    st.Issuer,
	}

	if st.IsExpired() {
		status.Status = "expired" //nolint:goconst
	} else {
		status.Status = "valid" //nolint:goconst
	}

	return status
}

// Logout removes the stored token for the given server URL.
func (c *Client) Logout(serverURL string) error {
	return c.store.DeleteToken(serverURL)
}

func buildAuthURL(authEndpoint, cID, redirectURI, state string, pkce PKCEChallenge) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {cID},
		"redirect_uri":          {redirectURI},
		"scope":                 {defaultScopes},
		"state":                 {state},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.ChallengeMethod},
	}
	return authEndpoint + "?" + params.Encode()
}

func exchangeCode(ctx context.Context, tokenEndpoint, cID, redirectURI, code, verifier string) (*Token, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {cID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if token.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &token, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
