package orchestrator

import (
	"context"
	"fmt"
	"io"

	"github.com/giantswarm/klausctl/pkg/runtime"
)

// EnsureImage makes image available to the container runtime before a
// container starts. A local image is only looked up in the runtime's image
// store and never pulled. Any other image is pulled with progress written to
// w; when the pull fails but a copy is cached locally (e.g. expired registry
// credentials), the cached copy is used.
func EnsureImage(ctx context.Context, rt runtime.Runtime, image string, local bool, w io.Writer) error {
	if local {
		images, err := rt.Images(ctx, image)
		if err != nil {
			return fmt.Errorf("looking up local image %s: %w", image, err)
		}
		if len(images) == 0 {
			return fmt.Errorf("local image %s not found in the %s image store; build or tag it first (%s images lists what is there)", image, rt.Name(), rt.Name())
		}
		_, _ = fmt.Fprintf(w, "Using local image %s.\n", image)
		return nil
	}

	_, _ = fmt.Fprintf(w, "Pulling %s...\n", image)
	if err := rt.Pull(ctx, image, w); err != nil {
		images, imgErr := rt.Images(ctx, image)
		if imgErr != nil || len(images) == 0 {
			return fmt.Errorf("pulling image: %w", err)
		}
		_, _ = fmt.Fprintln(w, "Pull failed, using locally cached image.")
	}
	return nil
}
