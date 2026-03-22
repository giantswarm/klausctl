package orchestrator

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
)

var imageCache struct {
	mu       sync.Mutex
	ref      string
	resolved time.Time
}

const imageCacheTTL = 5 * time.Minute

// ResolveDefaultImage resolves the default klaus base image to the latest
// semver-tagged version from the registry. If the image has already been
// resolved by the user or a personality, it is returned as-is.
//
// On resolution failure the function falls back to :latest and writes a
// warning to w.
func ResolveDefaultImage(ctx context.Context, client *klausoci.Client, image string, w io.Writer) string {
	if !config.IsDefaultImage(image) {
		return image
	}

	imageCache.mu.Lock()
	if imageCache.ref != "" && time.Since(imageCache.resolved) < imageCacheTTL {
		cached := imageCache.ref
		imageCache.mu.Unlock()
		return cached
	}
	imageCache.mu.Unlock()

	resolved, err := client.ResolveLatestVersion(ctx, config.DefaultImageRepository)
	if err != nil {
		fmt.Fprintf(w, "Warning: could not resolve latest klaus image tag: %v; falling back to :latest\n", err)
		return config.DefaultImageFallback
	}

	imageCache.mu.Lock()
	imageCache.ref = resolved
	imageCache.resolved = time.Now()
	imageCache.mu.Unlock()

	return resolved
}
