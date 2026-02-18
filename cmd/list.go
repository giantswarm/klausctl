package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var listOutput string

type listEntry struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Toolchain   string `json:"toolchain,omitempty"`
	Personality string `json:"personality,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Port        int    `json:"port,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List klaus instances",
	RunE:  runList,
}

func init() {
	listCmd.Flags().StringVarP(&listOutput, "output", "o", "text", "output format: text, json")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(listOutput); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}

	entries, err := loadListEntries(paths)
	if err != nil {
		return err
	}

	if listOutput == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No instances found. Use 'klausctl create <name>' to create one.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tTOOLCHAIN\tPERSONALITY\tWORKSPACE\tPORT\tUPTIME")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			e.Name,
			e.Status,
			valueOrDash(e.Toolchain),
			valueOrDash(e.Personality),
			valueOrDash(e.Workspace),
			e.Port,
			valueOrDash(e.Uptime),
		)
	}
	return w.Flush()
}

func loadListEntries(paths *config.Paths) ([]listEntry, error) {
	dirEntries, err := os.ReadDir(paths.InstancesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading instances directory: %w", err)
	}

	stateByName := map[string]*instance.Instance{}
	states, err := instance.LoadAll(paths)
	if err != nil {
		return nil, err
	}
	for _, st := range states {
		stateByName[st.Name] = st
	}

	list := make([]listEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		instPaths := paths.ForInstance(name)

		cfg, err := config.Load(instPaths.ConfigFile)
		if err != nil {
			// Skip malformed/incomplete directories.
			continue
		}

		item := listEntry{
			Name:        name,
			Status:      "stopped",
			Toolchain:   shortToolchain(cfg.Image),
			Personality: shortRefName(cfg.Personality),
			Workspace:   cfg.Workspace,
			Port:        cfg.Port,
		}

		if st, ok := stateByName[name]; ok {
			rt, err := runtime.New(st.Runtime)
			if err == nil {
				status, err := rt.Status(context.Background(), st.ContainerName())
				if err == nil && status != "" {
					item.Status = status
					if status == "running" {
						if info, err := rt.Inspect(context.Background(), st.ContainerName()); err == nil && !info.StartedAt.IsZero() {
							item.Uptime = formatDuration(time.Since(info.StartedAt))
						} else if !st.StartedAt.IsZero() {
							item.Uptime = formatDuration(time.Since(st.StartedAt))
						}
					}
				}
			}
		}

		list = append(list, item)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func shortToolchain(image string) string {
	repo := repositoryFromRef(image)
	name := filepath.Base(repo)
	if strings.HasPrefix(name, "klaus-") {
		return strings.TrimPrefix(name, "klaus-")
	}
	return name
}

func shortRefName(ref string) string {
	if ref == "" {
		return ""
	}
	return filepath.Base(repositoryFromRef(ref))
}

func valueOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
