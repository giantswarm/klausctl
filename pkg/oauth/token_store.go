package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// nowFunc is overridable in tests to control time.
var nowFunc = time.Now

func now() time.Time { return nowFunc() }

// TokenStore manages OAuth tokens persisted as JSON files keyed by server URL.
// Each server gets a file at <dir>/<url-safe-hash>.json with 0600 permissions.
type TokenStore struct {
	dir string
}

// NewTokenStore creates a TokenStore backed by the given directory. The
// directory is created on first write if it does not exist.
func NewTokenStore(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

// GetToken retrieves the stored token for the given server URL.
// Returns nil if no token is stored.
func (s *TokenStore) GetToken(serverURL string) *StoredToken {
	path := s.pathFor(serverURL)
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return nil
	}

	var st StoredToken
	if err := json.Unmarshal(data, &st); err != nil {
		return nil
	}
	return &st
}

// GetValidToken retrieves a non-expired token for the server URL.
// Returns nil if no token is stored or the token has expired.
func (s *TokenStore) GetValidToken(serverURL string) *StoredToken {
	st := s.GetToken(serverURL)
	if st == nil {
		return nil
	}
	if st.IsExpired() {
		return nil
	}
	return st
}

// StoreToken persists a token for the given server and issuer URLs.
func (s *TokenStore) StoreToken(serverURL, issuerURL string, token Token) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}

	st := StoredToken{
		Token:     token,
		Issuer:    issuerURL,
		ServerURL: serverURL,
		CreatedAt: now(),
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}

	return os.WriteFile(s.pathFor(serverURL), data, 0o600)
}

// DeleteToken removes the stored token for the given server URL.
func (s *TokenStore) DeleteToken(serverURL string) error {
	path := s.pathFor(serverURL)
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting token for %s: %w", serverURL, err)
	}
	return nil
}

// HasValidToken reports whether a non-expired token exists for the server.
func (s *TokenStore) HasValidToken(serverURL string) bool {
	return s.GetValidToken(serverURL) != nil
}

// TokenStatus describes the authentication state for a server.
type TokenStatus struct {
	ServerURL string
	Issuer    string
	Status    string // "valid", "expired", "none"
	ExpiresAt string // RFC3339 or empty
}

// ListTokens returns the status of all stored tokens sorted by server URL.
func (s *TokenStore) ListTokens() ([]TokenStatus, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tokens directory: %w", err)
	}

	var statuses []TokenStatus
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}

		var st StoredToken
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}

		status := TokenStatus{
			ServerURL: st.ServerURL,
			Issuer:    st.Issuer,
		}

		if st.IsExpired() {
			status.Status = "expired"
		} else {
			status.Status = "valid"
		}

		if st.Token.ExpiresIn > 0 {
			expiry := st.CreatedAt.Add(time.Duration(st.Token.ExpiresIn) * time.Second)
			status.ExpiresAt = expiry.Format(time.RFC3339)
		}

		statuses = append(statuses, status)
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ServerURL < statuses[j].ServerURL
	})

	return statuses, nil
}

func (s *TokenStore) pathFor(serverURL string) string {
	h := sha256.Sum256([]byte(serverURL))
	return filepath.Join(s.dir, hex.EncodeToString(h[:])+".json")
}
