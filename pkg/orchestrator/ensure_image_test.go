package orchestrator

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/runtime"
)

// imageRuntime is a runtime whose image store holds cached and whose pulls
// fail with pullErr. Methods EnsureImage does not call panic through the
// embedded nil interface.
type imageRuntime struct {
	runtime.Runtime
	cached  []string
	pullErr error
	pulled  []string
}

func (r *imageRuntime) Name() string { return "docker" }

func (r *imageRuntime) Pull(_ context.Context, image string, _ io.Writer) error {
	r.pulled = append(r.pulled, image)
	return r.pullErr
}

func (r *imageRuntime) Images(_ context.Context, filter string) ([]runtime.ImageInfo, error) {
	var out []runtime.ImageInfo
	for _, ref := range r.cached {
		if ref == filter {
			out = append(out, runtime.ImageInfo{Repository: ref})
		}
	}
	return out, nil
}

func TestEnsureImage(t *testing.T) {
	const image = "klaus:my-branch"
	pullFails := errors.New("unauthorized")

	tests := []struct {
		name       string
		rt         *imageRuntime
		local      bool
		wantErr    string
		wantPulled bool
	}{
		{name: "local image in the store is used without a pull", rt: &imageRuntime{cached: []string{image}}, local: true},
		{name: "missing local image is an error, never a pull", rt: &imageRuntime{}, local: true, wantErr: "local image klaus:my-branch not found in the docker image store"},
		{name: "registry image is pulled", rt: &imageRuntime{}, wantPulled: true},
		{name: "failed pull falls back to the cached copy", rt: &imageRuntime{cached: []string{image}, pullErr: pullFails}, wantPulled: true},
		{name: "failed pull without a cached copy is an error", rt: &imageRuntime{pullErr: pullFails}, wantPulled: true, wantErr: "pulling image: unauthorized"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureImage(context.Background(), tt.rt, image, tt.local, io.Discard)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("EnsureImage() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("EnsureImage() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if pulled := len(tt.rt.pulled) > 0; pulled != tt.wantPulled {
				t.Fatalf("pulled = %v, want %v", pulled, tt.wantPulled)
			}
		})
	}
}
