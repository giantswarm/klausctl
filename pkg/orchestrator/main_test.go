package orchestrator

import (
	"os"
	"testing"

	"github.com/giantswarm/klausctl/pkg/ocicache"
)

// TestMain disables the klaus-oci registry cache for every test in this
// package. Tests here stand up synthetic clients and must never share
// state with the developer's persistent cache, or the behaviour of
// fallback/error paths becomes non-deterministic.
func TestMain(m *testing.M) {
	ocicache.Configure("", true)
	os.Exit(m.Run())
}
