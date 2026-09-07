package project

import "testing"

// Fixtures for the fallback table; constants keep the linter's goconst quiet.
const (
	sha    = "abc1234"
	tagged = "v1.2.3"
	older  = "v0.9.0"
)

func TestVersionFallback(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		buildInfo string
		gitSHA    string
		want      string
	}{
		{"nothing available", dev, "", dev, dev},
		{"explicit version ldflag wins", tagged, older, sha, tagged},
		{"build info supplies version", dev, tagged, sha, tagged},
		{"build info absent; sha fallback", dev, "", sha, sha},
		{"build info beats sha", dev, tagged, sha, tagged},
	}

	origVersion, origSHA, origBuildInfo := version, gitSHA, buildInfoVersion
	t.Cleanup(func() { version, gitSHA, buildInfoVersion = origVersion, origSHA, origBuildInfo })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version = tc.version
			gitSHA = tc.gitSHA
			buildInfoVersion = func() string { return tc.buildInfo }
			if got := Version(); got != tc.want {
				t.Errorf("Version() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAccessors(t *testing.T) {
	if GitSHA() == "" {
		t.Error("GitSHA must not be empty")
	}
	if BuildTimestamp() == "" {
		t.Error("BuildTimestamp must not be empty")
	}
}
