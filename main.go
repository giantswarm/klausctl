package main

import (
	"fmt"
	"os"

	"github.com/giantswarm/klausctl/cmd"
	"github.com/giantswarm/klausctl/pkg/project"
)

func main() {
	// Build identity from pkg/project: the architect CI and the devctl
	// Makefile stamp it through -ldflags -X at link time (the Makefile always
	// targeted pkg/project, which did not exist, so release binaries printed
	// "dev"); a plain `go build` falls back to Go's VCS build info.
	cmd.SetBuildInfo(project.Version(), project.GitSHA(), project.BuildTimestamp())
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
