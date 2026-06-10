package config

import (
	"fmt"
	"os/exec"
	"strings"
)

// HostGitIdentity returns the git identity configured on the host
// (git config user.name / user.email), evaluated from dir so that
// per-repository identities take precedence over the global one.
// Either value may be empty when the host has no identity configured.
func HostGitIdentity(dir string) (name, email string) {
	return gitConfigGet(dir, "user.name"), gitConfigGet(dir, "user.email")
}

// HostSigningKey resolves the GPG signing key to use for commit signing.
// It prefers the host git configuration (user.signingkey, evaluated from
// dir) and falls back to the fingerprint of the secret key matching email.
func HostSigningKey(dir, email string) (string, error) {
	if key := gitConfigGet(dir, "user.signingkey"); key != "" {
		return key, nil
	}
	if email == "" {
		return "", fmt.Errorf("cannot resolve GPG signing key: git config user.signingkey is unset and no git author email is configured")
	}
	out, err := exec.Command("gpg", "--list-secret-keys", "--with-colons", email).Output() // #nosec G204 -- email comes from the user's own git config
	if err != nil {
		return "", fmt.Errorf("cannot resolve GPG signing key: git config user.signingkey is unset and gpg has no secret key for %q", email)
	}
	if fpr := firstSecretKeyFingerprint(string(out)); fpr != "" {
		return fpr, nil
	}
	return "", fmt.Errorf("cannot resolve GPG signing key: no secret key fingerprint found for %q", email)
}

// firstSecretKeyFingerprint extracts the fingerprint of the first secret
// key from gpg --with-colons output (the "fpr" record following a "sec"
// record).
func firstSecretKeyFingerprint(colons string) string {
	seenSec := false
	for _, line := range strings.Split(colons, "\n") {
		fields := strings.Split(line, ":")
		switch fields[0] {
		case "sec":
			seenSec = true
		case "fpr":
			if seenSec && len(fields) > 9 && fields[9] != "" {
				return fields[9]
			}
		}
	}
	return ""
}

func gitConfigGet(dir, key string) string {
	cmd := exec.Command("git", "config", "--get", key) // #nosec G204 -- key is a fixed literal passed by callers in this file
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
