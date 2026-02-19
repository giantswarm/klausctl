// Package instance implements MCP tool handlers for klaus instance lifecycle
// management: create, start, stop, delete, status, logs, and list.
package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/oci"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// RegisterTools registers all instance lifecycle tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerCreate(s, sc)
	registerStart(s, sc)
	registerStop(s, sc)
	registerDelete(s, sc)
	registerStatus(s, sc)
	registerLogs(s, sc)
	registerList(s, sc)
}

func registerCreate(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_create",
		mcp.WithDescription("Create and start a new klaus instance"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithString("workspace", mcp.Description("Workspace directory (default: current working directory)")),
		mcp.WithString("personality", mcp.Description("Personality short name or OCI reference")),
		mcp.WithString("toolchain", mcp.Description("Toolchain short name or OCI reference")),
		mcp.WithArray("plugin", mcp.Description("Additional plugin short names or OCI references")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleCreate(ctx, req, sc)
	})
}

func registerStart(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_start",
		mcp.WithDescription("Start a stopped klaus instance using its saved config"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStart(ctx, req, sc)
	})
}

func registerStop(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stop",
		mcp.WithDescription("Stop a running klaus instance"),
		mcp.WithString("name", mcp.Description("Instance name (required unless all=true)")),
		mcp.WithBoolean("all", mcp.Description("Stop all instances")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStop(ctx, req, sc)
	})
}

func registerDelete(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_delete",
		mcp.WithDescription("Stop and remove a klaus instance entirely (config, state, rendered files)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleDelete(ctx, req, sc)
	})
}

func registerStatus(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_status",
		mcp.WithDescription("Return instance status as JSON"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatus(ctx, req, sc)
	})
}

func registerLogs(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_logs",
		mcp.WithDescription("Return recent container log lines"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithNumber("tail", mcp.Description("Number of lines from end (default: 100)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleLogs(ctx, req, sc)
	})
}

func registerList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_list",
		mcp.WithDescription("List all instances with status, toolchain, personality, workspace, port, and uptime as JSON"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleList(ctx, req, sc)
	})
}

// --- Handlers ---

func handleCreate(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := config.ValidateInstanceName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	workspace := req.GetString("workspace", "")
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("determining current directory: %v", err)), nil
		}
		workspace = cwd
	}

	personality := req.GetString("personality", "")
	toolchain := req.GetString("toolchain", "")
	pluginArgs := req.GetStringSlice("plugin", nil)

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	instancePaths := sc.InstancePaths(name)
	if _, err := os.Stat(instancePaths.InstanceDir); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("instance %q already exists", name)), nil
	}

	cfg, err := config.GenerateInstanceConfig(sc.Paths, config.CreateOptions{
		Name:        name,
		Workspace:   workspace,
		Personality: personality,
		Toolchain:   toolchain,
		Plugins:     pluginArgs,
		Context:     ctx,
		Output:      io.Discard,
		ResolvePersonality: func(ctx context.Context, ref string, w io.Writer) (*config.ResolvedPersonality, error) {
			if err := config.EnsureDir(sc.Paths.PersonalitiesDir); err != nil {
				return nil, fmt.Errorf("creating personalities directory: %w", err)
			}
			pr, err := oci.ResolvePersonality(ctx, ref, sc.Paths.PersonalitiesDir, io.Discard)
			if err != nil {
				return nil, err
			}
			plugins := make([]config.Plugin, 0, len(pr.Spec.Plugins))
			for _, p := range pr.Spec.Plugins {
				plugins = append(plugins, oci.PluginFromReference(p))
			}
			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   pr.Spec.Image,
			}, nil
		},
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("generating config: %v", err)), nil
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating instance directory: %v", err)), nil
	}
	data, err := cfg.Marshal()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("serializing config: %v", err)), nil
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing instance config: %v", err)), nil
	}

	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating rendered directory parent: %v", err)), nil
	}

	result, err := startExistingInstance(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func handleStart(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	result, err := startExistingInstance(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(result)
}

func handleStop(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	all := req.GetBool("all", false)

	if name == "" && !all {
		return mcp.NewToolResultError("either name or all=true is required"), nil
	}
	if name != "" && all {
		return mcp.NewToolResultError("name and all=true are mutually exclusive"), nil
	}

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	if all {
		return stopAll(ctx, sc)
	}

	return stopOne(ctx, name, sc)
}

func handleDelete(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := config.ValidateInstanceName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	paths := sc.InstancePaths(name)
	if _, err := os.Stat(paths.InstanceDir); err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("instance %q does not exist", name)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	inst, _ := instance.Load(paths)
	if err := cleanupContainer(ctx, name, inst); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cleaning up container: %v", err)), nil
	}
	if err := os.RemoveAll(paths.InstanceDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("deleting instance directory: %v", err)), nil
	}

	return jsonResult(map[string]string{
		"instance": name,
		"status":   "deleted",
	})
}

type statusResult struct {
	Instance    string `json:"instance"`
	Status      string `json:"status"`
	Personality string `json:"personality,omitempty"`
	Container   string `json:"container"`
	Runtime     string `json:"runtime"`
	Image       string `json:"image"`
	Workspace   string `json:"workspace"`
	MCP         string `json:"mcp,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

func handleStatus(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("no instance found for %q; use klaus_create to create one", name)), nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	containerName := inst.ContainerName()
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		return mcp.NewToolResultError(fmt.Sprintf("instance %q has stale state (container no longer exists); use klaus_create to start a new one", name)), nil
	}

	result := statusResult{
		Instance:    inst.Name,
		Status:      status,
		Personality: inst.Personality,
		Container:   containerName,
		Runtime:     inst.Runtime,
		Image:       inst.Image,
		Workspace:   inst.Workspace,
	}

	if status == "running" {
		result.MCP = fmt.Sprintf("http://localhost:%d", inst.Port)
		if info, err := rt.Inspect(ctx, containerName); err == nil && !info.StartedAt.IsZero() {
			result.Uptime = formatDuration(time.Since(info.StartedAt))
		} else if !inst.StartedAt.IsZero() {
			result.Uptime = formatDuration(time.Since(inst.StartedAt))
		}
	}

	return jsonResult(result)
}

func handleLogs(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tail := int(req.GetFloat("tail", 100))

	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("no instance found for %q", name)), nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	logs, err := rt.LogsCapture(ctx, inst.ContainerName(), tail)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching logs: %v", err)), nil
	}

	return mcp.NewToolResultText(logs), nil
}

type listEntry struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Toolchain   string `json:"toolchain,omitempty"`
	Personality string `json:"personality,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Port        int    `json:"port,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

func handleList(ctx context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	if err := config.MigrateLayout(sc.Paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("migrating config layout: %v", err)), nil
	}

	dirEntries, err := os.ReadDir(sc.Paths.InstancesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonResult([]listEntry{})
		}
		return mcp.NewToolResultError(fmt.Sprintf("reading instances directory: %v", err)), nil
	}

	stateByName := map[string]*instance.Instance{}
	states, err := instance.LoadAll(sc.Paths)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading instance states: %v", err)), nil
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
		instPaths := sc.InstancePaths(name)

		cfg, err := config.Load(instPaths.ConfigFile)
		if err != nil {
			continue
		}

		item := listEntry{
			Name:        name,
			Status:      "stopped",
			Toolchain:   shortToolchainName(cfg),
			Personality: shortRefName(cfg.Personality),
			Workspace:   cfg.Workspace,
			Port:        cfg.Port,
		}

		if st, ok := stateByName[name]; ok {
			rt, err := runtime.New(st.Runtime)
			if err == nil {
				status, err := rt.Status(ctx, st.ContainerName())
				if err == nil && status != "" {
					item.Status = status
					if status == "running" {
						if info, err := rt.Inspect(ctx, st.ContainerName()); err == nil && !info.StartedAt.IsZero() {
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

	return jsonResult(list)
}

// --- Helpers ---

type createResult struct {
	Instance    string `json:"instance"`
	Status      string `json:"status"`
	Container   string `json:"container"`
	Image       string `json:"image"`
	Workspace   string `json:"workspace"`
	Port        int    `json:"port"`
	Personality string `json:"personality,omitempty"`
}

// startExistingInstance loads config for a named instance and starts its
// container. Used by both create and start handlers.
func startExistingInstance(ctx context.Context, name string, sc *server.ServerContext) (*createResult, error) {
	paths := sc.InstancePaths(name)
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("loading config for %q: %w", name, err)
	}

	workspace := config.ExpandPath(cfg.Workspace)
	if _, err := os.Stat(workspace); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("workspace directory does not exist: %s", workspace)
		}
		return nil, fmt.Errorf("checking workspace directory: %w", err)
	}

	rt, err := runtime.New(cfg.Runtime)
	if err != nil {
		return nil, err
	}

	containerName := instance.ContainerName(name)

	// Clean up stale containers.
	inst, err := instance.Load(paths)
	if err == nil && inst.Name != "" {
		status, sErr := rt.Status(ctx, inst.ContainerName())
		if sErr == nil && status == "running" {
			return nil, fmt.Errorf("instance %q is already running (container: %s, MCP: http://localhost:%d)", inst.Name, inst.ContainerName(), inst.Port)
		}
		_ = rt.Remove(ctx, inst.ContainerName())
		_ = instance.Clear(paths)
	}

	// Resolve personality if configured.
	var personalityDir string
	if cfg.Personality != "" {
		if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
			return nil, fmt.Errorf("creating personalities directory: %w", err)
		}
		pr, err := oci.ResolvePersonality(ctx, cfg.Personality, paths.PersonalitiesDir, io.Discard)
		if err != nil {
			return nil, fmt.Errorf("resolving personality: %w", err)
		}
		personalityDir = pr.Dir
		cfg.Plugins = oci.MergePlugins(pr.Spec.Plugins, cfg.Plugins)
		if !cfg.ImageExplicitlySet() && pr.Spec.Image != "" {
			cfg.Image = pr.Spec.Image
		}
	}

	image := cfg.Image

	r := renderer.New(paths)
	if err := r.Render(cfg); err != nil {
		return nil, fmt.Errorf("rendering config: %w", err)
	}

	if len(cfg.Plugins) > 0 {
		if err := oci.PullPlugins(ctx, cfg.Plugins, paths.PluginsDir, io.Discard); err != nil {
			return nil, fmt.Errorf("pulling plugins: %w", err)
		}
	}

	runOpts, err := orchestrator.BuildRunOptions(cfg, paths, containerName, image, personalityDir)
	if err != nil {
		return nil, fmt.Errorf("building run options: %w", err)
	}

	if err := rt.Pull(ctx, image, io.Discard); err != nil {
		return nil, fmt.Errorf("pulling image: %w", err)
	}

	containerID, err := rt.Run(ctx, runOpts)
	if err != nil {
		return nil, fmt.Errorf("starting container: %w", err)
	}

	inst = &instance.Instance{
		Name:        name,
		ContainerID: containerID,
		Runtime:     rt.Name(),
		Personality: cfg.Personality,
		Image:       image,
		Port:        cfg.Port,
		Workspace:   workspace,
		StartedAt:   time.Now(),
	}
	if err := inst.Save(paths); err != nil {
		return nil, fmt.Errorf("saving instance state: %w", err)
	}

	return &createResult{
		Instance:    name,
		Status:      "running",
		Container:   containerName,
		Image:       image,
		Workspace:   workspace,
		Port:        cfg.Port,
		Personality: cfg.Personality,
	}, nil
}

func stopOne(ctx context.Context, name string, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		return jsonResult(map[string]string{
			"instance": name,
			"status":   "not running",
		})
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	containerName := inst.ContainerName()
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		_ = instance.Clear(paths)
		return jsonResult(map[string]string{
			"instance": name,
			"status":   "not found (cleared stale state)",
		})
	}

	if status == "running" {
		if err := rt.Stop(ctx, containerName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stopping container: %v", err)), nil
		}
	}
	if err := rt.Remove(ctx, containerName); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("removing container: %v", err)), nil
	}
	if err := instance.Clear(paths); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("clearing instance state: %v", err)), nil
	}

	return jsonResult(map[string]string{
		"instance": name,
		"status":   "stopped",
	})
}

func stopAll(ctx context.Context, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	instances, err := instance.LoadAll(sc.Paths)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading instances: %v", err)), nil
	}

	stopped := make([]string, 0, len(instances))
	for _, inst := range instances {
		rt, err := runtime.New(inst.Runtime)
		if err != nil {
			continue
		}
		containerName := inst.ContainerName()
		status, err := rt.Status(ctx, containerName)
		if err != nil || status == "" {
			_ = instance.Clear(sc.InstancePaths(inst.Name))
			continue
		}
		if status == "running" {
			if err := rt.Stop(ctx, containerName); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("stopping %s: %v", containerName, err)), nil
			}
		}
		if err := rt.Remove(ctx, containerName); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("removing %s: %v", containerName, err)), nil
		}
		if err := instance.Clear(sc.InstancePaths(inst.Name)); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("clearing state for %s: %v", inst.Name, err)), nil
		}
		stopped = append(stopped, inst.Name)
	}

	return jsonResult(map[string]any{
		"status":  "all stopped",
		"stopped": stopped,
	})
}

func cleanupContainer(ctx context.Context, name string, inst *instance.Instance) error {
	containerName := instance.ContainerName(name)
	if inst != nil && inst.Name != "" {
		containerName = inst.ContainerName()
	}

	candidates := []string{}
	if inst != nil && inst.Runtime != "" {
		candidates = append(candidates, inst.Runtime)
	}
	for _, rtName := range []string{"docker", "podman"} {
		found := false
		for _, c := range candidates {
			if c == rtName {
				found = true
				break
			}
		}
		if !found {
			candidates = append(candidates, rtName)
		}
	}

	for _, rtName := range candidates {
		rt, err := runtime.New(rtName)
		if err != nil {
			continue
		}
		status, err := rt.Status(ctx, containerName)
		if err != nil || status == "" {
			continue
		}
		if status == "running" {
			if err := rt.Stop(ctx, containerName); err != nil {
				return fmt.Errorf("stopping container via %s: %w", rtName, err)
			}
		}
		if err := rt.Remove(ctx, containerName); err != nil {
			return fmt.Errorf("removing container via %s: %w", rtName, err)
		}
	}

	return nil
}

func shortToolchainName(cfg *config.Config) string {
	ref := cfg.Toolchain
	if ref == "" {
		ref = cfg.Image
	}
	repo := repositoryFromRef(ref)
	name := filepath.Base(repo)
	if len(name) > 6 && name[:6] == "klaus-" {
		return name[6:]
	}
	return name
}

func shortRefName(ref string) string {
	if ref == "" {
		return ""
	}
	return filepath.Base(repositoryFromRef(ref))
}

func repositoryFromRef(ref string) string {
	if idx := indexOf(ref, "@"); idx > 0 {
		return ref[:idx]
	}
	lastSlash := lastIndexOf(ref, "/")
	lastColon := lastIndexOf(ref, ":")
	if lastColon > lastSlash {
		return ref[:lastColon]
	}
	return ref
}

func indexOf(s, sub string) int {
	for i := range s {
		if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
