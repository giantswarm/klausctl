package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAuthStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewAuthStore(filepath.Join(dir, "auth"))

	rec := AuthRecord{ // #nosec G101 -- constant identifier, not a credential
		ServerURL:     "https://gw.example.com",
		Issuer:        "https://auth.example.com",
		AccessToken:   "at-123",
		TokenType:     "Bearer",
		RefreshToken:  "rt-abc",
		ExpiresAt:     time.Now().Add(1 * time.Hour).UTC().Round(time.Second),
		Scope:         "openid profile",
		TokenEndpoint: "https://auth.example.com/oauth/token",
		ClientID:      "cimd://client-id-url",
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(rec.ServerURL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatalf("expected record, got nil")
	}
	if got.AccessToken != rec.AccessToken || got.RefreshToken != rec.RefreshToken {
		t.Errorf("tokens not preserved: got %+v want %+v", got, rec)
	}
	if got.TokenEndpoint != rec.TokenEndpoint || got.ClientID != rec.ClientID {
		t.Errorf("token endpoint/client id not preserved: got %+v", got)
	}
	if !got.ExpiresAt.Equal(rec.ExpiresAt) {
		t.Errorf("expires_at: got %v want %v", got.ExpiresAt, rec.ExpiresAt)
	}
}

func TestAuthStoreGetMissingReturnsNil(t *testing.T) {
	store := NewAuthStore(t.TempDir())
	got, err := store.Get("https://gw.example.com")
	if err != nil {
		t.Fatalf("Get on empty store: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil record for missing host, got %+v", got)
	}
}

func TestAuthStoreFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions only")
	}
	dir := filepath.Join(t.TempDir(), "auth")
	store := NewAuthStore(dir)
	rec := AuthRecord{ServerURL: "https://gw.example.com", AccessToken: "tok"}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	fileName, _ := hostFilename(rec.ServerURL)
	fi, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file perm = %o, want 0600", got)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("dir perm = %o, want 0700", got)
	}
}

func TestAuthStoreDelete(t *testing.T) {
	store := NewAuthStore(t.TempDir())
	rec := AuthRecord{ServerURL: "https://gw.example.com", AccessToken: "tok"}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(rec.ServerURL); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Second delete should be a no-op.
	if err := store.Delete(rec.ServerURL); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	got, _ := store.Get(rec.ServerURL)
	if got != nil {
		t.Errorf("expected record removed, still got %+v", got)
	}
}

func TestAuthStoreList(t *testing.T) {
	store := NewAuthStore(t.TempDir())
	recs := []AuthRecord{
		{ServerURL: "https://a.example.com", AccessToken: "a"},
		{ServerURL: "https://b.example.com", AccessToken: "b"},
		{ServerURL: "https://c.example.com:8080", AccessToken: "c"},
	}
	for _, r := range recs {
		if err := store.Put(r); err != nil {
			t.Fatalf("Put %s: %v", r.ServerURL, err)
		}
	}
	got, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != len(recs) {
		t.Fatalf("List returned %d entries, want %d", len(got), len(recs))
	}
	var urls []string
	for _, r := range got {
		urls = append(urls, r.ServerURL)
	}
	sort.Strings(urls)
	if urls[0] != "https://a.example.com" || urls[1] != "https://b.example.com" || urls[2] != "https://c.example.com:8080" {
		t.Errorf("List returned unexpected URLs: %v", urls)
	}
}

func TestAuthStoreListEmptyDir(t *testing.T) {
	store := NewAuthStore(filepath.Join(t.TempDir(), "does-not-exist"))
	got, err := store.List()
	if err != nil {
		t.Fatalf("List on missing dir: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on empty store, got %+v", got)
	}
}

func TestAuthStorePutRejectsEmptyServerURL(t *testing.T) {
	store := NewAuthStore(t.TempDir())
	if err := store.Put(AuthRecord{AccessToken: "x"}); err == nil {
		t.Fatalf("expected error for empty server_url")
	}
}

func TestHostFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://gw.example.com", "gw.example.com.yaml"},
		{"https://GW.Example.com", "gw.example.com.yaml"},
		{"https://gw.example.com:8080", "gw.example.com_8080.yaml"},
		{"http://localhost:8080", "localhost_8080.yaml"},
	}
	for _, tc := range cases {
		got, err := hostFilename(tc.in)
		if err != nil {
			t.Errorf("hostFilename(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("hostFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHostFilenameRejectsEmpty(t *testing.T) {
	if _, err := hostFilename(""); err == nil {
		t.Fatal("expected error on empty URL")
	}
	if _, err := hostFilename("not-a-url"); err == nil {
		t.Fatal("expected error on URL with no host")
	}
}

func TestAuthRecordIsExpired(t *testing.T) {
	zero := &AuthRecord{}
	if zero.IsExpired(0) {
		t.Error("zero ExpiresAt should be treated as non-expiring")
	}
	past := &AuthRecord{ExpiresAt: time.Now().Add(-1 * time.Minute)}
	if !past.IsExpired(0) {
		t.Error("past ExpiresAt should be expired")
	}
	future := &AuthRecord{ExpiresAt: time.Now().Add(10 * time.Minute)}
	if future.IsExpired(0) {
		t.Error("future ExpiresAt with zero leeway should not be expired")
	}
	if !future.IsExpired(20 * time.Minute) {
		t.Error("leeway larger than remaining time should mark as expired")
	}
}

func TestWriteFileAtomic0600CreatesNoTempLeftover(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions only")
	}
	dir := filepath.Join(t.TempDir(), "auth")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "x.yaml")
	if err := writeFileAtomic0600(path, []byte("data")); err != nil {
		t.Fatalf("writeFileAtomic0600: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".auth-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}
