package cmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var (
	logsFollow bool
	logsTail   int
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream container logs",
	Long:  `Stream logs from the running klaus container.`,
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 0, "number of lines to show from the end of the logs (0 = all)")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(_ *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	inst, err := instance.Load(paths)
	if err != nil {
		return err
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	return rt.Logs(ctx, inst.ContainerName(), logsFollow, logsTail)
}
