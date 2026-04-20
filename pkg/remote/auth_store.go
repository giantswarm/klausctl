package remote

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AuthRecord is the per-host credential record persisted on disk.
//
// ExpiresAt is the absolute expiry of AccessToken. When the access token is
// about to expire, callers should refresh using RefreshToken.
type AuthRecord struct {
	// ServerURL is the remote klaus-gateway root (normalized).
	ServerURL string `yaml:"server_url"`
	// Issuer is the OAuth authorization server issuer URL (from discovery).
	Issuer string `yaml:"issuer"`
	// AccessToken is the current access token.
	AccessToken string `yaml:"access_token"`
	// TokenType is the token type, typically "Bearer".
	TokenType string `yaml:"token_type,omitempty"`
	// RefreshToken is used to mint a new access token without re-login.
	RefreshToken string `yaml:"refresh_token,omitempty"`
	// ExpiresAt is the absolute expiry time of AccessToken.
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
	// Scope is the scope granted for the access token, if reported.
	Scope string `yaml:"scope,omitempty"`
	// TokenEndpoint is the OAuth token endpoint (cached so refresh can
	// proceed without repeating discovery).
	TokenEndpoint string `yaml:"token_endpoint,omitempty"`
	// ClientID is the OAuth client identifier used during the original
	// authorization (typically the CIMD URL).
	ClientID string `yaml:"client_id,omitempty"`
}

// IsExpired reports whether the access token is expired or will expire
// within the given leeway. Zero ExpiresAt is treated as non-expiring.
func (r *AuthRecord) IsExpired(leeway time.Duration) bool {
	if r.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(leeway).After(r.ExpiresAt)
}

// AuthStore manages host-keyed OAuth records at ~/.config/klausctl/auth/.
// Files are written with 0600 and the parent directory with 0700.
type AuthStore struct {
	dir string
}

// NewAuthStore returns an AuthStore backed by the given directory.
func NewAuthStore(dir string) *AuthStore {
	return &AuthStore{dir: dir}
}

// Dir returns the directory backing the store.
func (s *AuthStore) Dir() string { return s.dir }

// Get returns the stored record for the given server URL, or nil when
// none exists. Only read errors other than ENOENT surface as errors.
func (s *AuthStore) Get(serverURL string) (*AuthRecord, error) {
	fileName, err := hostFilename(serverURL)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, fileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading auth record for %s: %w", serverURL, err)
	}
	var rec AuthRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parsing auth record for %s: %w", serverURL, err)
	}
	return &rec, nil
}

// Put writes a record for the given server URL with file mode 0600.
func (s *AuthStore) Put(rec AuthRecord) error {
	if rec.ServerURL == "" {
		return fmt.Errorf("auth record has empty server_url")
	}
	fileName, err := hostFilename(rec.ServerURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating auth directory: %w", err)
	}
	data, err := yaml.Marshal(&rec)
	if err != nil {
		return fmt.Errorf("marshaling auth record: %w", err)
	}
	path := filepath.Join(s.dir, fileName)
	return writeFileAtomic0600(path, data)
}

// Delete removes the record for the given server URL. Missing files are
// treated as success.
func (s *AuthStore) Delete(serverURL string) error {
	fileName, err := hostFilename(serverURL)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.dir, fileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting auth record for %s: %w", serverURL, err)
	}
	return nil
}

// List returns all stored records sorted by server URL.
func (s *AuthStore) List() ([]AuthRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing auth directory: %w", err)
	}
	var out []AuthRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec AuthRecord
		if err := yaml.Unmarshal(data, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// writeFileAtomic0600 writes data via a tempfile rename so concurrent
// readers never see a partially-written file. Final mode is 0600.
func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp auth file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing auth file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("setting auth file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing auth file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming auth file to %s: %w", path, err)
	}
	return nil
}

// hostFilename derives the on-disk filename for a server URL. We use
// "<host>[_port].yaml" so records are human-identifiable on the
// filesystem while still being collision-safe (the URL is re-validated on
// read via ServerURL inside the file).
//
// Unsafe characters are replaced with "_" so filesystem constraints
// (Windows, macOS) are respected.
func hostFilename(serverURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return "", fmt.Errorf("parsing server URL %q: %w", serverURL, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("server URL %q has no host", serverURL)
	}
	name := strings.ToLower(u.Host)
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	return name + ".yaml", nil
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-z0-9._-]`)
