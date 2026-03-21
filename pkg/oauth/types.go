// Package oauth provides OAuth 2.0 authentication for remote MCP servers.
// It implements browser-based authorization code flow with PKCE, token
// storage, server probing, and OAuth metadata discovery.
package oauth

import "time"

// Token holds the raw OAuth token fields returned by the authorization server.
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// StoredToken wraps a Token with metadata needed for storage and refresh.
type StoredToken struct {
	Token     Token     `json:"token"`
	Issuer    string    `json:"issuer"`
	ServerURL string    `json:"server_url"`
	CreatedAt time.Time `json:"created_at"`
}

// IsExpired reports whether the access token has expired based on the
// stored creation time and expires_in value. Returns false when no
// expiry information is available (treat as non-expiring).
func (st *StoredToken) IsExpired() bool {
	if st.Token.ExpiresIn <= 0 {
		return false
	}
	return time.Since(st.CreatedAt) > time.Duration(st.Token.ExpiresIn)*time.Second
}

// Metadata holds OAuth authorization server metadata from
// RFC 8414 (.well-known/oauth-authorization-server) or
// OpenID Connect discovery (.well-known/openid-configuration).
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
	CodeChallengeSupported []string `json:"code_challenge_methods_supported,omitempty"`
}

// AuthChallenge represents parsed fields from a WWW-Authenticate: Bearer
// response header.
type AuthChallenge struct {
	Realm            string
	ResourceMetadata string
}

// ProtectedResourceMetadata holds RFC 9728 OAuth Protected Resource Metadata
// fetched from .well-known/oauth-protected-resource.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	BearerMethods        []string `json:"bearer_methods_supported,omitempty"`
}

// PKCEChallenge holds a PKCE verifier/challenge pair for the S256 method.
type PKCEChallenge struct {
	Verifier        string
	Challenge       string
	ChallengeMethod string
}
