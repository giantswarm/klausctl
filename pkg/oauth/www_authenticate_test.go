package oauth

import "testing"

func TestParseWWWAuthenticate(t *testing.T) {
	tests := []struct {
		name             string
		header           string
		wantNil          bool
		wantRealm        string
		wantResourceMeta string
	}{
		{
			name:      "realm only",
			header:    `Bearer realm="https://dex.example.com"`,
			wantRealm: testIssuer,
		},
		{
			name:             "realm and resource_metadata",
			header:           `Bearer realm="https://dex.example.com", resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`,
			wantRealm:        testIssuer,
			wantResourceMeta: testResourceMetadataURL,
		},
		{
			name:             "no space after comma",
			header:           `Bearer realm="https://dex.example.com",resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`,
			wantRealm:        testIssuer,
			wantResourceMeta: testResourceMetadataURL,
		},
		{
			name:      "case insensitive Bearer",
			header:    `bearer realm="https://dex.example.com"`,
			wantRealm: testIssuer,
		},
		{
			name:      "case insensitive BEARER",
			header:    `BEARER realm="https://dex.example.com"`,
			wantRealm: testIssuer,
		},
		{
			name:      "unquoted realm",
			header:    `Bearer realm=https://dex.example.com`,
			wantRealm: testIssuer,
		},
		{
			name:      "extra whitespace",
			header:    `  Bearer   realm = "https://dex.example.com"  `,
			wantRealm: testIssuer,
		},
		{
			name:    "not Bearer scheme",
			header:  `Basic realm="test"`,
			wantNil: true,
		},
		{
			name:    "empty header",
			header:  "",
			wantNil: true,
		},
		{
			name:    "Bearer with no params",
			header:  "Bearer ",
			wantNil: true,
		},
		{
			name:             "resource_metadata only",
			header:           `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`,
			wantResourceMeta: testResourceMetadataURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWWWAuthenticate(tt.header)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.Realm != tt.wantRealm {
				t.Errorf("Realm = %q, want %q", got.Realm, tt.wantRealm)
			}
			if got.ResourceMetadata != tt.wantResourceMeta {
				t.Errorf("ResourceMetadata = %q, want %q", got.ResourceMetadata, tt.wantResourceMeta)
			}
		})
	}
}
