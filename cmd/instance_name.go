package cmd

import (
	"fmt"
	"io"

	"github.com/giantswarm/klausctl/pkg/config"
)

func resolveOptionalInstanceName(args []string, commandName string, errOut io.Writer) (string, error) {
	if len(args) > 0 {
		name := args[0]
		return name, config.ValidateInstanceName(name)
	}

	fmt.Fprintf(
		errOut,
		"%s omitting <name> is deprecated and will be removed in a future release; use 'klausctl %s default'.\n",
		yellow("Deprecation:"),
		commandName,
	)
	return "default", nil
}
