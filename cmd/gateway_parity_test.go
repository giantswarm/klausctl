package cmd

import (
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/pflag"

	"github.com/giantswarm/klausctl/internal/gatewaysurface"
	internalserver "github.com/giantswarm/klausctl/internal/server"
	gatewaytools "github.com/giantswarm/klausctl/internal/tools/gateway"
	"github.com/giantswarm/klausctl/pkg/config"
)

// TestGatewayStartFlagParity asserts every flag declared on `gateway start`
// has an MCP input equivalent on klaus_gateway_start, and vice versa -- the
// acceptance criterion for this feature.
func TestGatewayStartFlagParity(t *testing.T) {
	cliFlags := collectCLIFlagNames(gatewayStartCmd.Flags())
	surfaceCLI := cliFlagNames(gatewaysurface.StartFlags)

	assertSetEqual(t, "CLI flags on `klausctl gateway start`", cliFlags, surfaceCLI)

	mcpKeys := collectMCPToolProperties(t, "klaus_gateway_start")
	surfaceMCP := mcpKeyNames(gatewaysurface.StartFlags)

	assertSetEqual(t, "MCP inputs on klaus_gateway_start", mcpKeys, surfaceMCP)

	// Every surface entry ties a CLI flag to an MCP key one-to-one.
	if len(cliFlags) != len(mcpKeys) {
		t.Errorf("CLI flag count (%d) differs from MCP input count (%d)", len(cliFlags), len(mcpKeys))
	}
}

// TestGatewayStatusFlagParity asserts the same invariant for the status surface.
func TestGatewayStatusFlagParity(t *testing.T) {
	cliFlags := collectCLIFlagNames(gatewayStatusCmd.Flags())
	surfaceCLI := cliFlagNames(gatewaysurface.StatusFlags)

	assertSetEqual(t, "CLI flags on `klausctl gateway status`", cliFlags, surfaceCLI)

	mcpKeys := collectMCPToolProperties(t, "klaus_gateway_status")
	surfaceMCP := mcpKeyNames(gatewaysurface.StatusFlags)

	assertSetEqual(t, "MCP inputs on klaus_gateway_status", mcpKeys, surfaceMCP)

	if len(cliFlags) != len(mcpKeys) {
		t.Errorf("CLI flag count (%d) differs from MCP input count (%d)", len(cliFlags), len(mcpKeys))
	}
}

// TestGatewayMCPToolsRegistered sanity-checks that all three MCP tools are
// exposed with the expected names.
func TestGatewayMCPToolsRegistered(t *testing.T) {
	srv := newToolsServer(t)
	tools := srv.ListTools()

	want := []string{"klaus_gateway_start", "klaus_gateway_stop", "klaus_gateway_status"}
	for _, name := range want {
		if _, ok := tools[name]; !ok {
			t.Errorf("expected MCP tool %q to be registered; got %v", name, toolNames(tools))
		}
	}
}

func TestGatewayCommandRegistered(t *testing.T) {
	assertCommandOnRoot(t, "gateway")
}

// collectCLIFlagNames returns the set of declared flag names on a pflag.FlagSet.
func collectCLIFlagNames(fs *pflag.FlagSet) map[string]struct{} {
	names := map[string]struct{}{}
	fs.VisitAll(func(f *pflag.Flag) {
		names[f.Name] = struct{}{}
	})
	return names
}

// collectMCPToolProperties returns the set of input property names registered
// on the named MCP tool.
func collectMCPToolProperties(t *testing.T, toolName string) map[string]struct{} {
	t.Helper()
	srv := newToolsServer(t)
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

// newToolsServer builds a fresh MCP server with klaus-gateway tools registered
// against it. The backing ServerContext uses ephemeral paths so nothing on the
// host filesystem is touched.
func newToolsServer(t *testing.T) *server.MCPServer {
	t.Helper()
	srv := server.NewMCPServer("klausctl-test", "test")
	sc := &internalserver.ServerContext{
		Paths: &config.Paths{
			ConfigDir: t.TempDir(),
		},
	}
	gatewaytools.RegisterTools(srv, sc)
	return srv
}

func cliFlagNames(flags []gatewaysurface.Flag) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range flags {
		out[f.CLIFlag] = struct{}{}
	}
	return out
}

func mcpKeyNames(flags []gatewaysurface.Flag) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range flags {
		out[f.MCPKey] = struct{}{}
	}
	return out
}

func assertSetEqual(t *testing.T, label string, got, want map[string]struct{}) {
	t.Helper()
	missing := difference(want, got)
	extra := difference(got, want)
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%s missing expected entries: %v", label, missing)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("%s has unexpected entries: %v", label, extra)
	}
}

func difference(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

func toolNames(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
