package orchestrator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

func TestResolveDefaultImage_ExplicitImage(t *testing.T) {
	// An explicitly set image should be returned as-is without any registry call.
	explicit := "myregistry.io/myimage:v1.0.0"
	var buf bytes.Buffer
	got := ResolveDefaultImage(context.Background(), NewDefaultClient(), explicit, &buf)
	if got != explicit {
		t.Errorf("ResolveDefaultImage() = %q, want %q", got, explicit)
	}
	if buf.Len() > 0 {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestResolveDefaultImage_FallbackOnError(t *testing.T) {
	// Use an unreachable registry to trigger the fallback.
	client := NewDefaultClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel so the registry call fails

	var buf bytes.Buffer
	got := ResolveDefaultImage(ctx, client, config.DefaultImageRepository, &buf)
	if got != config.DefaultImageFallback {
		t.Errorf("ResolveDefaultImage() = %q, want fallback %q", got, config.DefaultImageFallback)
	}
	if !strings.Contains(buf.String(), "Warning") {
		t.Errorf("expected warning output, got %q", buf.String())
	}
}

func TestIsDefaultImage(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{config.DefaultImageRepository, true},
		{config.DefaultImageFallback, true},
		{"gsoci.azurecr.io/giantswarm/klaus:v0.0.86", false},
		{"myregistry.io/myimage:latest", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := config.IsDefaultImage(tt.image); got != tt.want {
				t.Errorf("IsDefaultImage(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}
