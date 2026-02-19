package cmd

import (
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/internal/server"
	artifacttools "github.com/giantswarm/klausctl/internal/tools/artifact"
	instancetools "github.com/giantswarm/klausctl/internal/tools/instance"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the MCP server over stdio",
	Long: `Run an MCP (Model Context Protocol) server over stdio, exposing klausctl's
container lifecycle and artifact management as MCP tools.

This enables IDE agents (Cursor, Claude Code) to create, manage, and inspect
local klaus instances directly from within the editor.

Configure in your IDE:

  Cursor (.cursor/mcp.json):
    {"mcpServers":{"klausctl":{"command":"klausctl","args":["serve"]}}}

  Claude Code (settings):
    {"mcpServers":{"klausctl":{"command":"klausctl","args":["serve"]}}}`,
	SilenceUsage: true,
	RunE:         runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return err
	}

	agentClient := mcpclient.New()
	defer agentClient.Close()

	serverCtx := &server.ServerContext{
		Paths:     paths,
		MCPClient: agentClient,
	}

	mcpSrv := mcpserver.NewMCPServer(
		"klausctl",
		buildVersion,
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithInstructions(serverInstructions()),
	)

	instancetools.RegisterTools(mcpSrv, serverCtx)
	artifacttools.RegisterTools(mcpSrv, serverCtx)

	return mcpserver.ServeStdio(mcpSrv)
}

func serverInstructions() string {
	return `klausctl manages local klaus containers backed by Docker or Podman.

Use the instance tools to create, start, stop, delete, and inspect local klaus instances.
Use the artifact tools to discover available toolchains, personalities, and plugins.
Use the agent tools to send prompts to and retrieve results from running instances.

Typical workflow:
1. Use klaus_list to see existing instances.
2. Use klaus_create to create a new instance with a name, workspace, and optional personality/toolchain.
3. Use klaus_status to check if an instance is running (includes agent status).
4. Use klaus_prompt to send a task to an agent instance.
5. Use klaus_result to retrieve the agent's response.
6. Use klaus_logs to inspect container output when troubleshooting.
7. Use klaus_stop or klaus_delete to clean up.`
}
