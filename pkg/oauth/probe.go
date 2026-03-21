package oauth

import (
	"context"
	"fmt"
	"net/http"
)

// ProbeServer sends a HEAD request to the given MCP server URL and checks
// for a 401 Unauthorized response with a WWW-Authenticate: Bearer header.
// Returns nil if the server does not require authentication. Falls back to
// checking .well-known/oauth-protected-resource (RFC 9728) when the HEAD
// response lacks a WWW-Authenticate header.
func ProbeServer(ctx context.Context, serverURL string) (*AuthChallenge, error) {
	challenge, err := probeViaHEAD(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	if challenge != nil {
		return challenge, nil
	}

	return probeViaResourceMetadata(ctx, serverURL)
}

func probeViaHEAD(ctx context.Context, serverURL string) (*AuthChallenge, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating probe request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probing %s: %w", serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, nil
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		return nil, nil
	}

	return ParseWWWAuthenticate(wwwAuth), nil
}

func probeViaResourceMetadata(ctx context.Context, serverURL string) (*AuthChallenge, error) {
	metaURL := serverURL + "/.well-known/oauth-protected-resource"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating resource metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	return &AuthChallenge{
		ResourceMetadata: metaURL,
	}, nil
}
