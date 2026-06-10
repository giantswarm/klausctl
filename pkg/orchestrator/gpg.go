package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// containerGNUPGHome is where the prepared GNUPGHOME is mounted inside the
// container. GNUPGHOME is pointed here so gpg finds both the public keyring
// and the forwarded agent socket.
const containerGNUPGHome = "/etc/klaus/gnupg"

// buildGPGVolumes prepares commit signing for the container when
// git.signCommits is enabled:
//
//   - a minimal GNUPGHOME containing only the exported public key is
//     rendered on the host and mounted read-write (gpg needs lock files),
//   - the host gpg-agent's restricted "extra" socket is mounted at
//     $GNUPGHOME/S.gpg-agent, so private key operations stay on the host
//     (the restricted socket disallows key export and admin commands).
//
// Returns nil volumes when signing is disabled.
func buildGPGVolumes(cfg *config.Config, paths *config.Paths, env map[string]string) ([]runtime.Volume, error) {
	if !cfg.Git.SignCommits {
		return nil, nil
	}
	key := cfg.Git.SigningKey
	if key == "" {
		return nil, fmt.Errorf("git.signCommits is enabled but git.signingKey is empty; re-create the instance or set git.signingKey in the instance config")
	}

	socket, err := hostAgentExtraSocket()
	if err != nil {
		return nil, err
	}

	home, err := renderGNUPGHome(paths.RenderedDir, key)
	if err != nil {
		return nil, err
	}

	env["GNUPGHOME"] = containerGNUPGHome
	return []runtime.Volume{
		{HostPath: home, ContainerPath: containerGNUPGHome},
		{HostPath: socket, ContainerPath: containerGNUPGHome + "/S.gpg-agent"},
	}, nil
}

// hostAgentExtraSocket ensures the host gpg-agent is running and returns the
// path of its restricted ("extra") socket.
func hostAgentExtraSocket() (string, error) {
	if out, err := exec.Command("gpgconf", "--launch", "gpg-agent").CombinedOutput(); err != nil {
		return "", fmt.Errorf("launching host gpg-agent: %v: %s", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("gpgconf", "--list-dir", "agent-extra-socket").Output()
	if err != nil {
		return "", fmt.Errorf("locating gpg-agent extra socket: %w", err)
	}
	socket := strings.TrimSpace(string(out))
	if _, err := os.Stat(socket); err != nil {
		return "", fmt.Errorf("gpg-agent extra socket not available at %s: %w", socket, err)
	}
	return socket, nil
}

// renderGNUPGHome builds a fresh GNUPGHOME under renderedDir containing only
// the public key for signingKey, exported from the host keyring.
func renderGNUPGHome(renderedDir, signingKey string) (string, error) {
	home := filepath.Join(renderedDir, "gnupg")
	if err := os.RemoveAll(home); err != nil {
		return "", fmt.Errorf("cleaning rendered GNUPGHOME: %w", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("creating rendered GNUPGHOME: %w", err)
	}

	// Force the file-based keyring: a fresh homedir on modern gnupg would
	// otherwise default to keyboxd, whose socket is unreachable from the
	// container.
	if err := os.WriteFile(filepath.Join(home, "common.conf"), []byte{}, 0o600); err != nil {
		return "", fmt.Errorf("writing common.conf: %w", err)
	}

	pub, err := exec.Command("gpg", "--export", signingKey).Output() // #nosec G204 -- signingKey comes from the instance config the user controls
	if err != nil {
		return "", fmt.Errorf("exporting public key %q from host keyring: %w", signingKey, err)
	}
	if len(pub) == 0 {
		return "", fmt.Errorf("host keyring has no public key for %q", signingKey)
	}

	pubPath := filepath.Join(home, "pubkey.gpg")
	if err := os.WriteFile(pubPath, pub, 0o600); err != nil {
		return "", fmt.Errorf("writing exported public key: %w", err)
	}
	if out, err := exec.Command("gpg", "--homedir", home, "--batch", "--no-autostart", "--import", pubPath).CombinedOutput(); err != nil { // #nosec G204 -- home is a klausctl-rendered path
		return "", fmt.Errorf("importing public key into rendered GNUPGHOME: %v: %s", err, strings.TrimSpace(string(out)))
	}

	return home, nil
}
