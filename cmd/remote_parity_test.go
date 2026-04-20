package cmd

import (
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/remotesurface"
	internalserver "github.com/giantswarm/klausctl/internal/server"
	instancetools "github.com/giantswarm/klausctl/internal/tools/instance"
	"github.com/giantswarm/klausctl/pkg/config"
)

// TestRemoteSurfaceFlagParity asserts that the `remote` and `session` inputs
// declared in internal/remotesurface are wired identically to every CLI
// subcommand (run/prompt/messages) and every matching MCP tool (klaus_run,
// klaus_prompt, klaus_messages). This is the acceptance criterion extending
// PR #196's gateway parity check to the new remote-targeting surface.
func TestRemoteSurfaceFlagParity(t *testing.T) {
	surfaceCLI := remoteCLINames()
	surfaceMCP := remoteMCPNames()

	if len(surfaceCLI) != len(surfaceMCP) {
		t.Fatalf("remotesurface.Flags: CLI names (%d) differ from MCP keys (%d)", len(surfaceCLI), len(surfaceMCP))
	}

	instanceSrv := newInstanceToolsServer(t)

	cases := []struct {
		label    string
		cliFlags map[string]struct{}
		mcpProps map[string]struct{}
	}{
		{
			label:    "run / klaus_run",
			cliFlags: collectCLIFlagNames(runCmd.Flags()),
			mcpProps: mcpToolProperties(t, instanceSrv, "klaus_run"),
		},
		{
			label:    "prompt / klaus_prompt",
			cliFlags: collectCLIFlagNames(promptCmd.Flags()),
			mcpProps: mcpToolProperties(t, instanceSrv, "klaus_prompt"),
		},
		{
			label:    "messages / klaus_messages",
			cliFlags: collectCLIFlagNames(messagesCmd.Flags()),
			mcpProps: mcpToolProperties(t, instanceSrv, "klaus_messages"),
		},
	}

	for _, c := range cases {
		assertSubset(t, "CLI flags on `klausctl "+c.label+"`", surfaceCLI, c.cliFlags)
		assertSubset(t, "MCP inputs on "+c.label, surfaceMCP, c.mcpProps)
	}
}

// TestRemoteSurfaceFlagsPresent guarantees the shared surface package itself
// still declares exactly `remote` and `session`. Adding or removing an entry
// here must be paired with updated CLI bindings and MCP registrations.
func TestRemoteSurfaceFlagsPresent(t *testing.T) {
	want := map[string]struct{}{"remote": {}, "session": {}}
	assertSetEqual(t, "remotesurface.Flags CLI names", remoteCLINames(), want)
	assertSetEqual(t, "remotesurface.Flags MCP keys", remoteMCPNames(), want)
}

func remoteCLINames() map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range remotesurface.Flags {
		out[f.CLIFlag] = struct{}{}
	}
	return out
}

func remoteMCPNames() map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range remotesurface.Flags {
		out[f.MCPKey] = struct{}{}
	}
	return out
}

// newInstanceToolsServer builds a fresh MCP server with instance tools
// registered against it. The backing ServerContext uses ephemeral paths so
// nothing on the host filesystem is touched.
func newInstanceToolsServer(t *testing.T) *server.MCPServer {
	t.Helper()
	srv := server.NewMCPServer("klausctl-test", "test")
	sc := &internalserver.ServerContext{
		Paths: &config.Paths{
			ConfigDir: t.TempDir(),
		},
	}
	instancetools.RegisterTools(srv, sc)
	return srv
}

// mcpToolProperties returns the set of input property names registered on
// the named MCP tool against an already-built server.
func mcpToolProperties(t *testing.T, srv *server.MCPServer, toolName string) map[string]struct{} {
	t.Helper()
	tools := srv.ListTools()
	tool, ok := tools[toolName]
	if !ok {
		t.Fatalf("MCP tool %q is not registered; available tools: %v", toolName, toolNames(tools))
	}
	props := map[string]struct{}{}
	for k := range tool.Tool.InputSchema.Properties {
		props[k] = struct{}{}
	}
	return props
}

// assertSubset verifies every key in `want` is present in `got`. Unlike
// assertSetEqual, extra entries in `got` are permitted (e.g. run/prompt/
// messages carry many flags beyond the shared remote surface).
func assertSubset(t *testing.T, label string, want, got map[string]struct{}) {
	t.Helper()
	missing := difference(want, got)
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	t.Errorf("%s missing expected entries: %v", label, missing)
}
