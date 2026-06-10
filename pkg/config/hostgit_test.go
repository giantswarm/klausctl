package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstSecretKeyFingerprint(t *testing.T) {
	colons := `sec:u:4096:1:C8A8E3F23BAB4599:1468411498::::::scESC:::+:::23::0:
fpr:::::::::43F38C03302CCD02D29EC068C8A8E3F23BAB4599:
uid:u::::1468411498::ABCDEF::Test User <test@example.com>::::::::::0:
ssb:u:4096:1:0123456789ABCDEF:1468411498::::::e:::+:::23:
fpr:::::::::FEDCBA9876543210FEDCBA9876543210FEDCBA98:
`
	got := firstSecretKeyFingerprint(colons)
	want := "43F38C03302CCD02D29EC068C8A8E3F23BAB4599"
	if got != want {
		t.Errorf("firstSecretKeyFingerprint = %q, want %q", got, want)
	}
}

func TestFirstSecretKeyFingerprint_NoSecretKey(t *testing.T) {
	if got := firstSecretKeyFingerprint("pub:u:4096:1:AAAA:1::::::e:\nfpr:::::::::BBBB:\n"); got != "" {
		t.Errorf("expected empty fingerprint, got %q", got)
	}
}

func TestHostGitIdentity_FromGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	gitconfig := filepath.Join(dir, "gitconfig")
	content := "[user]\n\tname = Test User\n\temail = test@example.com\n"
	if err := os.WriteFile(gitconfig, []byte(content), 0o600); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	name, email := HostGitIdentity(dir)
	if name != "Test User" {
		t.Errorf("name = %q, want %q", name, "Test User")
	}
	if email != "test@example.com" {
		t.Errorf("email = %q, want %q", email, "test@example.com")
	}
}

func TestHostSigningKey_FromGitConfig(t *testing.T) {
	dir := t.TempDir()
	gitconfig := filepath.Join(dir, "gitconfig")
	content := "[user]\n\tsigningkey = CAFEBABE\n"
	if err := os.WriteFile(gitconfig, []byte(content), 0o600); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	key, err := HostSigningKey(dir, "test@example.com")
	if err != nil {
		t.Fatalf("HostSigningKey: %v", err)
	}
	if key != "CAFEBABE" {
		t.Errorf("key = %q, want %q", key, "CAFEBABE")
	}
}
