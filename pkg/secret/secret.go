// Package secret provides a simple encrypted-at-rest secret store for klausctl.
// Secrets are stored as a flat YAML map in ~/.config/klausctl/secrets.yaml with
// 0600 file permissions.
package secret

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

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

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat secrets file: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("secrets file %s has permissions %04o; expected 0600 (owner-only)", path, perm)
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

// Set stores or updates a named secret.
func (s *Store) Set(name, value string) {
	s.secrets[name] = value
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
