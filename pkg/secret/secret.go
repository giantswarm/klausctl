// Package secret provides a file-permission-protected secret store for klausctl.
// Secrets are stored as a flat YAML map in ~/.config/klausctl/secrets.yaml with
// owner-only (0600) file permissions.
package secret

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Store manages named secrets persisted as a YAML file with restricted
// file permissions.
type Store struct {
	path    string
	secrets map[string]string
}

// Load reads secrets from the given file path. If the file does not exist,
// an empty store is returned. An error is returned when the file exists but
// cannot be read or parsed, or when file permissions are too open.
func Load(path string) (*Store, error) {
	s := &Store{
		path:    path,
		secrets: make(map[string]string),
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("opening secrets file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat secrets file: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("secrets file %s has permissions %04o; expected 0600 (owner-only)", path, perm)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}

	if err := yaml.Unmarshal(data, &s.secrets); err != nil {
		return nil, fmt.Errorf("parsing secrets file: %w", err)
	}
	if s.secrets == nil {
		s.secrets = make(map[string]string)
	}

	return s, nil
}

// Save writes the current secrets to disk with 0600 permissions.
func (s *Store) Save() error {
	data, err := yaml.Marshal(s.secrets)
	if err != nil {
		return fmt.Errorf("marshaling secrets: %w", err)
	}
	return os.WriteFile(s.path, data, 0o600)
}

// ValidateName checks that a secret name is safe for use as both a map key
// and a filename. Names must start with an alphanumeric character and contain
// only alphanumerics, dots, hyphens, or underscores.
func ValidateName(name string) error {
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: must match %s", name, validNameRe.String())
	}
	return nil
}

// Set stores or updates a named secret. Returns an error if the name is invalid.
func (s *Store) Set(name, value string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	s.secrets[name] = value
	return nil
}

// Get retrieves a secret by name. Returns an error when the name is not found.
func (s *Store) Get(name string) (string, error) {
	v, ok := s.secrets[name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", name)
	}
	return v, nil
}

// Delete removes a named secret. Returns an error when the name is not found.
func (s *Store) Delete(name string) error {
	if _, ok := s.secrets[name]; !ok {
		return fmt.Errorf("secret %q not found", name)
	}
	delete(s.secrets, name)
	return nil
}

// List returns all secret names in sorted order.
func (s *Store) List() []string {
	names := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
