package oci

import (
	"testing"
)

func TestSplitRegistryBase(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		wantHost   string
		wantPrefix string
	}{
		{
			name:       "standard plugin registry",
			base:       "gsoci.azurecr.io/giantswarm/klaus-plugins",
			wantHost:   "gsoci.azurecr.io",
			wantPrefix: "giantswarm/klaus-plugins/",
		},
		{
			name:       "standard personality registry",
			base:       "gsoci.azurecr.io/giantswarm/klaus-personalities",
			wantHost:   "gsoci.azurecr.io",
			wantPrefix: "giantswarm/klaus-personalities/",
		},
		{
			name:       "host only",
			base:       "gsoci.azurecr.io",
			wantHost:   "gsoci.azurecr.io",
			wantPrefix: "",
		},
		{
			name:       "localhost with port",
			base:       "localhost:5000/plugins",
			wantHost:   "localhost:5000",
			wantPrefix: "plugins/",
		},
		{
			name:       "deep path",
			base:       "example.com/org/team/artifacts",
			wantHost:   "example.com",
			wantPrefix: "org/team/artifacts/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotPrefix := SplitRegistryBase(tt.base)
			if gotHost != tt.wantHost {
				t.Errorf("host = %q, want %q", gotHost, tt.wantHost)
			}
			if gotPrefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", gotPrefix, tt.wantPrefix)
			}
		})
	}
}

func TestSplitRegistryBase_LocalhostWithPort(t *testing.T) {
	host, prefix := SplitRegistryBase("localhost:5000/test/repos")
	if host != "localhost:5000" {
		t.Errorf("host = %q, want %q", host, "localhost:5000")
	}
	if prefix != "test/repos/" {
		t.Errorf("prefix = %q, want %q", prefix, "test/repos/")
	}
}
