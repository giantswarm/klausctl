package cmd

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var (
	logsFollow bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream container logs",
	Long:  `Stream logs from the running klaus container.`,
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(_ *cobra.Command, _ []string) error {
	inst, err := instance.Load()
	if err != nil {
		return err
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	return rt.Logs(inst.ContainerName(), logsFollow)
}
