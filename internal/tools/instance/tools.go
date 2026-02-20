// Package instance implements MCP tool handlers for klaus instance lifecycle
// management: create, start, stop, delete, status, logs, and list.
package instance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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
	registerPrompt(s, sc)
	registerResult(s, sc)
}

func registerCreate(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_create",
		mcp.WithDescription("Create and start a new klaus instance"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithString("workspace", mcp.Description("Workspace directory (default: current working directory)")),
		mcp.WithString("personality", mcp.Description("Personality short name or OCI reference")),
		mcp.WithString("toolchain", mcp.Description("Toolchain short name or OCI reference")),
		mcp.WithArray("plugin", mcp.Description("Additional plugin short names or OCI references")),
		mcp.WithObject("envVars", mcp.Description("Environment variable key-value pairs to set in the container (merged with any existing envVars from the resolved config)")),
		mcp.WithArray("envForward", mcp.Description("Host environment variable names to forward to the container (merged with any existing envForward entries; duplicates are removed)")),
		mcp.WithObject("mcpServers", mcp.Description("MCP server configurations rendered to .mcp.json (merged with any existing mcpServers from the resolved config)")),
		mcp.WithNumber("maxBudgetUsd", mcp.Description("Maximum dollar budget for the Claude agent per invocation (0 = no limit)")),
		mcp.WithString("permissionMode", mcp.Description("Claude permission mode: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")),
		mcp.WithString("model", mcp.Description("Claude model (e.g. sonnet, opus, claude-sonnet-4-20250514)")),
		mcp.WithString("systemPrompt", mcp.Description("System prompt override for the Claude agent")),
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

	personality, toolchain, pluginArgs, err = oci.ResolveCreateRefs(ctx, personality, toolchain, pluginArgs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving refs: %v", err)), nil
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
			plugins, err := oci.ResolvePluginRefs(ctx, oci.PluginRefsFromSpec(pr.Spec.Plugins))
			if err != nil {
				return nil, fmt.Errorf("resolving personality plugins: %w", err)
			}
			image, err := oci.ResolveArtifactRef(ctx, pr.Spec.Image, oci.DefaultToolchainRegistry, "klaus-")
			if err != nil {
				return nil, fmt.Errorf("resolving personality image: %w", err)
			}

			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   image,
			}, nil
		},
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("generating config: %v", err)), nil
	}

	if err := applyCreateOverrides(req, cfg); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
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
		_ = os.RemoveAll(instancePaths.InstanceDir)
		return mcp.NewToolResultError(err.Error()), nil
	}
	return server.JSONResult(result)
}

// applyCreateOverrides extracts optional config overrides from the MCP request
// and applies them to the generated config before it is persisted.
func applyCreateOverrides(req mcp.CallToolRequest, cfg *config.Config) error {
	args := req.GetArguments()

	if raw, ok := args["envVars"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("envVars must be an object with string values")
		}
		if cfg.EnvVars == nil {
			cfg.EnvVars = make(map[string]string, len(m))
		}
		for k, v := range m {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("envVars value for %q must be a string", k)
			}
			cfg.EnvVars[k] = s
		}
	}

	if fwd := req.GetStringSlice("envForward", nil); len(fwd) > 0 {
		cfg.EnvForward = append(cfg.EnvForward, fwd...)
		slices.Sort(cfg.EnvForward)
		cfg.EnvForward = slices.Compact(cfg.EnvForward)
	}

	if raw, ok := args["mcpServers"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("mcpServers must be an object")
		}
		if cfg.McpServers == nil {
			cfg.McpServers = make(map[string]any, len(m))
		}
		for k, v := range m {
			cfg.McpServers[k] = v
		}
	}

	if budget := req.GetFloat("maxBudgetUsd", 0); budget != 0 {
		cfg.Claude.MaxBudgetUSD = budget
	}

	if pm := req.GetString("permissionMode", ""); pm != "" {
		cfg.Claude.PermissionMode = pm
	}

	if model := req.GetString("model", ""); model != "" {
		cfg.Claude.Model = model
	}

	if sp := req.GetString("systemPrompt", ""); sp != "" {
		cfg.Claude.SystemPrompt = sp
	}

	return cfg.Validate()
}

func handleStart(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := startExistingInstance(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return server.JSONResult(result)
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

	paths := sc.InstancePaths(name)
	if _, err := os.Stat(paths.InstanceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

	return server.JSONResult(map[string]string{
		"instance": name,
		"status":   "deleted",
	})
}

type statusResult struct {
	Instance    string `json:"instance"`
	Status      string `json:"status"`
	AgentStatus string `json:"agent_status,omitempty"`
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

	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		cfg, cfgErr := config.Load(paths.ConfigFile)
		if cfgErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("no instance found for %q; use klaus_create to create one", name)), nil
		}
		return server.JSONResult(statusResult{
			Instance:  name,
			Status:    "stopped",
			Container: instance.ContainerName(name),
			Runtime:   cfg.Runtime,
			Image:     cfg.Image,
			Workspace: cfg.Workspace,
		})
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
		if agentStatus := queryAgentStatus(ctx, inst.Name, inst.Port, sc); agentStatus != "" {
			result.AgentStatus = agentStatus
		}
	}

	return server.JSONResult(result)
}

func handleLogs(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tail := int(req.GetFloat("tail", 100))

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
	dirEntries, err := os.ReadDir(sc.Paths.InstancesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return server.JSONResult([]listEntry{})
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

	return server.JSONResult(list)
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
		if errors.Is(err, os.ErrNotExist) {
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
			resolved, err := oci.ResolveArtifactRef(ctx, pr.Spec.Image, oci.DefaultToolchainRegistry, "")
			if err != nil {
				return nil, fmt.Errorf("resolving personality image: %w", err)
			}
			cfg.Image = resolved
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
		images, imgErr := rt.Images(ctx, image)
		if imgErr != nil || len(images) == 0 {
			return nil, fmt.Errorf("pulling image: %w", err)
		}
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
		return server.JSONResult(map[string]string{
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
		return server.JSONResult(map[string]string{
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

	return server.JSONResult(map[string]string{
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

	return server.JSONResult(map[string]any{
		"status":  "all stopped",
		"stopped": stopped,
	})
}

func cleanupContainer(ctx context.Context, name string, inst *instance.Instance) error {
	containerName := instance.ContainerName(name)
	if inst != nil && inst.Name != "" {
		containerName = inst.ContainerName()
	}

	candidates := uniqueRuntimes(inst)
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

func uniqueRuntimes(inst *instance.Instance) []string {
	all := []string{"docker", "podman"}
	if inst == nil || inst.Runtime == "" {
		return all
	}
	result := []string{inst.Runtime}
	for _, rt := range all {
		if rt != inst.Runtime {
			result = append(result, rt)
		}
	}
	return result
}

func shortToolchainName(cfg *config.Config) string {
	ref := cfg.Toolchain
	if ref == "" {
		ref = cfg.Image
	}
	repo := oci.RepositoryFromRef(ref)
	name := filepath.Base(repo)
	return strings.TrimPrefix(name, "klaus-")
}

func shortRefName(ref string) string {
	if ref == "" {
		return ""
	}
	return filepath.Base(oci.RepositoryFromRef(ref))
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
