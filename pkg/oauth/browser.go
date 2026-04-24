package oauth

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser opens the given URL in the user's default browser.
// Only http and https schemes are allowed.
func OpenBrowser(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing to open URL with scheme %q: only http and https are allowed", u.Scheme)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", rawURL) // #nosec G204 -- container runtime CLI invocation with controlled args
	case "darwin":
		cmd = exec.Command("open", rawURL) // #nosec G204 -- container runtime CLI invocation with controlled args
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", rawURL) // #nosec G204 -- container runtime CLI invocation with controlled args
	default:
		return fmt.Errorf("unsupported platform %q for opening browser", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}

	return nil
}
