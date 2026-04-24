package remote

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https root", "https://gw.example.com", "https://gw.example.com", false},
		{"trailing slash", "https://gw.example.com/", "https://gw.example.com", false},
		{"trailing v1", "https://gw.example.com/v1", "https://gw.example.com", false},
		{"trailing v1 slash", "https://gw.example.com/v1/", "https://gw.example.com", false},
		{"with subpath", "https://gw.example.com/klaus", "https://gw.example.com/klaus", false},
		{"query and fragment dropped", "https://gw.example.com/?x=1#f", "https://gw.example.com", false},
		{"whitespace trimmed", "  https://gw.example.com  ", "https://gw.example.com", false},
		{"http allowed", "http://localhost:8080", "http://localhost:8080", false},
		{"empty", "", "", true},
		{"no scheme", "gw.example.com", "", true},
		{"ftp scheme", "ftp://gw.example.com", "", true},
		{"no host", "https://", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeBaseURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTargetURLComposition(t *testing.T) {
	tgt, err := NewTarget("https://gw.example.com/v1/", "dev-abc", "feature-x", "")
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if got, want := tgt.CompletionsURL(), "https://gw.example.com/v1/dev-abc/chat/completions"; got != want {
		t.Errorf("CompletionsURL = %q, want %q", got, want)
	}
	if got, want := tgt.MCPURL(), "https://gw.example.com/v1/dev-abc/mcp"; got != want {
		t.Errorf("MCPURL = %q, want %q", got, want)
	}
	if got, want := tgt.BaseURL, "https://gw.example.com"; got != want {
		t.Errorf("BaseURL = %q, want %q (trailing /v1 should have been stripped)", got, want)
	}
	if tgt.ThreadID != "feature-x" {
		t.Errorf("ThreadID = %q, want explicit session %q", tgt.ThreadID, "feature-x")
	}
}

func TestTargetURLEscapesInstance(t *testing.T) {
	tgt, err := NewTarget("https://gw.example.com", "name with space", "s", "")
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if !strings.Contains(tgt.CompletionsURL(), "/v1/name%20with%20space/chat/completions") {
		t.Errorf("instance not escaped in URL: %s", tgt.CompletionsURL())
	}
}

func TestNewTargetRequiresInstance(t *testing.T) {
	if _, err := NewTarget("https://gw.example.com", "", "", ""); err == nil {
		t.Fatalf("expected error when instance is empty")
	}
}

func TestTargetHeaders(t *testing.T) {
	tgt := Target{
		BaseURL:   "https://gw.example.com",
		Instance:  "dev",
		ChannelID: "laptop.local",
		UserID:    "alice",
		ThreadID:  "thread-xyz",
	}
	h := tgt.Headers()
	if h[ChannelHeader] != ChannelCLI {
		t.Errorf("expected channel=cli, got %q", h[ChannelHeader])
	}
	if h[ChannelIDHeader] != "laptop.local" {
		t.Errorf("channel-id header missing or wrong: %q", h[ChannelIDHeader])
	}
	if h[UserIDHeader] != "alice" {
		t.Errorf("user-id header missing or wrong: %q", h[UserIDHeader])
	}
	if h[ThreadIDHeader] != "thread-xyz" {
		t.Errorf("thread-id header missing or wrong: %q", h[ThreadIDHeader])
	}
}

func TestTargetHeadersOmitsEmptyFields(t *testing.T) {
	tgt := Target{BaseURL: "https://gw.example.com", Instance: "dev"}
	h := tgt.Headers()
	if _, ok := h[ChannelIDHeader]; ok {
		t.Errorf("channel-id should be omitted when empty: %v", h)
	}
	if _, ok := h[UserIDHeader]; ok {
		t.Errorf("user-id should be omitted when empty: %v", h)
	}
	if _, ok := h[ThreadIDHeader]; ok {
		t.Errorf("thread-id should be omitted when empty: %v", h)
	}
	if h[ChannelHeader] != ChannelCLI {
		t.Errorf("channel header should always be present: %v", h)
	}
}

func TestResolveUserIDPrefersJWTSubject(t *testing.T) {
	// Build an unsigned token whose payload is base64url-encoded JSON
	// with sub="alice@example.com".
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"alice@example.com"}`))
	token := "header." + payload + ".sig"

	t.Setenv("USER", "fallback")
	if got := ResolveUserID(token); got != "alice@example.com" {
		t.Errorf("ResolveUserID(jwt) = %q, want %q (JWT sub should win over $USER)", got, "alice@example.com")
	}
}

func TestResolveUserIDFallsBackToEnv(t *testing.T) {
	t.Setenv("USER", "bob")
	if got := ResolveUserID(""); got != "bob" {
		t.Errorf("ResolveUserID(\"\") = %q, want %q", got, "bob")
	}
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "logname-user")
	if got := ResolveUserID(""); got != "logname-user" {
		t.Errorf("ResolveUserID empty USER = %q, want %q", got, "logname-user")
	}
}

func TestResolveUserIDUnknownWhenNothingSet(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	if got := ResolveUserID(""); got != "unknown" { //nolint:goconst
		t.Errorf("ResolveUserID with nothing set = %q, want %q", got, "unknown")
	}
}

func TestDefaultSessionStableFromCwd(t *testing.T) {
	a := DefaultSession()
	b := DefaultSession()
	if a != b {
		t.Errorf("DefaultSession is not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "cwd-") {
		t.Errorf("DefaultSession should start with cwd- prefix: %q", a)
	}
}

func TestNewTargetUsesExplicitSession(t *testing.T) {
	tgt, err := NewTarget("https://gw.example.com", "dev", "   my-thread  ", "")
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	if tgt.ThreadID != "my-thread" {
		t.Errorf("ThreadID = %q, want trimmed %q", tgt.ThreadID, "my-thread")
	}
}
