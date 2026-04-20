// Package remote implements the client-side wiring for targeting a
// klaus-gateway instead of a locally-running klaus container.
//
// The Target type composes the request URL, the routing headers
// (X-Klaus-Channel, X-Klaus-Channel-ID, X-Klaus-User-ID, X-Klaus-Thread-ID),
// and the bearer token used by `klausctl run`, `klausctl prompt`, and
// `klausctl messages` when `--remote=URL` is set.
package remote

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	// ChannelHeader identifies where a gateway request originated.
	ChannelHeader = "X-Klaus-Channel"
	// ChannelIDHeader scopes the channel to a specific identity (hostname).
	ChannelIDHeader = "X-Klaus-Channel-ID"
	// UserIDHeader carries the authenticated user identity.
	UserIDHeader = "X-Klaus-User-ID"
	// ThreadIDHeader carries the conversation/session identity.
	ThreadIDHeader = "X-Klaus-Thread-ID"

	// ChannelCLI is the value sent on ChannelHeader for klausctl CLI calls.
	ChannelCLI = "cli"
)

// Target describes a remote klaus-gateway endpoint plus the routing
// identities attached to every request.
type Target struct {
	// BaseURL is the gateway root, for example "https://gw.example.com".
	BaseURL string

	// Instance is the klaus instance name — becomes the `{instance}` path
	// segment in completions/MCP URLs.
	Instance string

	// BearerToken is attached as `Authorization: Bearer <token>` when set.
	BearerToken string

	// ChannelID (hostname) identifies the caller's machine.
	ChannelID string

	// UserID identifies the authenticated user (JWT `sub` or $USER).
	UserID string

	// ThreadID identifies the conversation/session (defaults to a stable
	// hash of the working directory when the user does not set --session).
	ThreadID string
}

// CompletionsURL is the OpenAI-compatible chat-completions endpoint for
// this target: `<base>/v1/<instance>/chat/completions`.
func (t Target) CompletionsURL() string {
	return joinURL(t.BaseURL, "/v1/", pathEscape(t.Instance), "/chat/completions")
}

// MCPURL is the streamable-HTTP MCP endpoint for this target:
// `<base>/v1/<instance>/mcp`.
func (t Target) MCPURL() string {
	return joinURL(t.BaseURL, "/v1/", pathEscape(t.Instance), "/mcp")
}

// Headers returns the routing header map attached to every gateway call.
// Callers should merge this into their request headers; empty fields are
// omitted so the gateway can apply defaults.
func (t Target) Headers() map[string]string {
	h := map[string]string{
		ChannelHeader: ChannelCLI,
	}
	if t.ChannelID != "" {
		h[ChannelIDHeader] = t.ChannelID
	}
	if t.UserID != "" {
		h[UserIDHeader] = t.UserID
	}
	if t.ThreadID != "" {
		h[ThreadIDHeader] = t.ThreadID
	}
	return h
}

// NewTarget composes a Target from a remote URL, instance name, session
// override (may be empty) and bearer token (may be empty). Channel ID and
// user ID are resolved from the host environment.
func NewTarget(remoteURL, instance, session, bearer string) (Target, error) {
	base, err := NormalizeBaseURL(remoteURL)
	if err != nil {
		return Target{}, err
	}
	if instance == "" {
		return Target{}, fmt.Errorf("instance name is required for remote target")
	}

	return Target{
		BaseURL:     base,
		Instance:    instance,
		BearerToken: bearer,
		ChannelID:   ResolveChannelID(),
		UserID:      ResolveUserID(bearer),
		ThreadID:    resolveThreadID(session),
	}, nil
}

// NormalizeBaseURL validates a remote URL and strips any trailing slash
// and any `/v1` suffix so path composition stays predictable.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("remote URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing remote URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("remote URL %q must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("remote URL %q has no host", raw)
	}

	// Trim trailing slash and a trailing `/v1` segment so CompletionsURL
	// never produces `/v1/v1/...`.
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/v1")
	u.Path = p
	u.RawQuery = ""
	u.Fragment = ""

	return strings.TrimRight(u.String(), "/"), nil
}

// ResolveChannelID returns the host identity used in X-Klaus-Channel-ID.
// Falls back to "unknown" when the hostname cannot be resolved.
func ResolveChannelID() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

// ResolveUserID extracts the user identity used in X-Klaus-User-ID. It
// prefers the `sub` claim from the bearer token JWT (if the token parses
// cleanly), falling back to $USER, then the current uid, then "unknown".
func ResolveUserID(bearer string) string {
	if sub := jwtSubject(bearer); sub != "" {
		return sub
	}
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("LOGNAME")); u != "" {
		return u
	}
	return "unknown"
}

// jwtSubject parses an unsigned JWT (or any token whose middle segment is
// a base64url-encoded JSON object) and returns its `sub` claim. Returns
// an empty string on any failure.
func jwtSubject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some IdPs emit padded base64; be lenient.
		if decoded, derr := base64.URLEncoding.DecodeString(parts[1]); derr == nil {
			payload = decoded
		} else {
			return ""
		}
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

// resolveThreadID normalizes the --session override or, when empty, falls
// back to a stable hash of the current working directory.
func resolveThreadID(session string) string {
	if s := strings.TrimSpace(session); s != "" {
		return s
	}
	return DefaultSession()
}

// DefaultSession returns the stable default thread id derived from the
// current working directory. Same directory -> same thread id across
// invocations, so a user running `klausctl prompt --remote=...` twice
// from the same repo reuses the same conversation thread.
func DefaultSession() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "default"
	}
	sum := sha256.Sum256([]byte(wd))
	return "cwd-" + hex.EncodeToString(sum[:])[:16]
}

// joinURL concatenates URL segments, ensuring exactly one slash between
// adjacent pieces. Segments may already carry their own slashes.
func joinURL(parts ...string) string {
	var out strings.Builder
	for i, p := range parts {
		if i == 0 {
			out.WriteString(strings.TrimRight(p, "/"))
			continue
		}
		if !strings.HasPrefix(p, "/") {
			out.WriteByte('/')
		}
		out.WriteString(strings.TrimRight(p, "/"))
	}
	return out.String()
}

// pathEscape percent-encodes an instance name for inclusion in a URL path.
// Instance names are DNS-label-shaped today, but escape defensively so a
// future loosening of the rules doesn't break the URL builder.
func pathEscape(s string) string {
	return url.PathEscape(s)
}
