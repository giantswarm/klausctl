package oauth

import (
	"strings"
	"testing"
)

func TestOpenBrowser_RejectsNonHTTPSchemes(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "javascript", url: "javascript:alert(1)", wantErr: true},
		{name: "file", url: "file:///etc/passwd", wantErr: true},
		{name: "ftp", url: "ftp://example.com", wantErr: true},
		{name: "data", url: "data:text/html,<h1>hi</h1>", wantErr: true},
		{name: "empty scheme", url: "://example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := OpenBrowser(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for scheme %q", tt.url)
			}
		})
	}
}

func TestOpenBrowser_AcceptsHTTPSchemes(t *testing.T) {
	// We can't actually open a browser in tests, but we can verify the URL
	// validation passes for valid schemes. The exec.Command will fail in CI
	// but that's after the validation check we're testing.
	tests := []struct {
		name string
		url  string
	}{
		{name: "http", url: "http://localhost:3001/callback"},
		{name: "https", url: "https://dex.example.com/auth?client_id=klausctl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := OpenBrowser(tt.url)
			// The error, if any, should NOT be about the scheme.
			if err != nil {
				if strings.Contains(err.Error(), "scheme") {
					t.Errorf("valid scheme rejected: %v", err)
				}
			}
		})
	}
}

func TestOpenBrowser_InvalidURL(t *testing.T) {
	err := OpenBrowser("://invalid")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}
