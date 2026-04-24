package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type cachedMetadata struct {
	meta      *Metadata
	fetchedAt time.Time
}

var (
	metadataCache   = make(map[string]cachedMetadata)
	metadataCacheMu sync.Mutex
	cacheTTL        = 10 * time.Minute
)

// DiscoverMetadata fetches OAuth authorization server metadata from the
// issuer URL. It tries RFC 8414 (.well-known/oauth-authorization-server)
// first, then falls back to OpenID Connect discovery
// (.well-known/openid-configuration). Results are cached with a 10-minute
// TTL.
func DiscoverMetadata(ctx context.Context, issuerURL string) (*Metadata, error) {
	issuerURL = strings.TrimRight(issuerURL, "/")

	metadataCacheMu.Lock()
	if cached, ok := metadataCache[issuerURL]; ok && time.Since(cached.fetchedAt) < cacheTTL {
		metadataCacheMu.Unlock()
		return cached.meta, nil
	}
	metadataCacheMu.Unlock()

	endpoints := []string{
		issuerURL + "/.well-known/oauth-authorization-server",
		issuerURL + "/.well-known/openid-configuration",
	}

	var lastErr error
	for _, endpoint := range endpoints {
		meta, err := fetchMetadata(ctx, endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
			lastErr = fmt.Errorf("metadata from %s missing required endpoints", endpoint)
			continue
		}

		metadataCacheMu.Lock()
		metadataCache[issuerURL] = cachedMetadata{meta: meta, fetchedAt: time.Now()}
		metadataCacheMu.Unlock()

		return meta, nil
	}

	return nil, fmt.Errorf("discovering OAuth metadata for %s: %w", issuerURL, lastErr)
}

func fetchMetadata(ctx context.Context, url string) (*Metadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	var meta Metadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata from %s: %w", url, err)
	}
	return &meta, nil
}

// FetchResourceMetadata fetches RFC 9728 OAuth Protected Resource Metadata
// from the given URL. This is used when a server's WWW-Authenticate header
// contains resource_metadata instead of realm.
func FetchResourceMetadata(ctx context.Context, metadataURL string) (*ProtectedResourceMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", metadataURL, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", metadataURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", metadataURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", metadataURL, err)
	}

	var meta ProtectedResourceMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("parsing resource metadata from %s: %w", metadataURL, err)
	}
	return &meta, nil
}

// ClearMetadataCache removes all cached metadata entries.
// Exported for testing.
func ClearMetadataCache() {
	metadataCacheMu.Lock()
	metadataCache = make(map[string]cachedMetadata)
	metadataCacheMu.Unlock()
}
