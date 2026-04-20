// Package gatewaysurface declares the public surface (flags and MCP inputs)
// of the `klausctl gateway` command group.
//
// Both the Cobra command (cmd/gateway.go) and the MCP tool registration
// (internal/tools/gateway) consume this package so the two surfaces cannot
// drift. A parity test in cmd/gateway_parity_test.go asserts every CLI flag
// has an MCP equivalent and vice versa.
package gatewaysurface

// Flag is a single CLI/MCP surface entry. CLIFlag is the Cobra flag name
// (kebab-case), MCPKey is the MCP input parameter name (camelCase), Kind
// names the value type ("bool", "int", "string"), and Description is the
// human-readable summary shown in both surfaces.
type Flag struct {
	CLIFlag     string
	MCPKey      string
	Kind        string
	Description string
}

// StartFlags lists the inputs accepted by `gateway start` and the
// `klaus_gateway_start` MCP tool.
var StartFlags = []Flag{
	{CLIFlag: "with-agentgateway", MCPKey: "withAgentgateway", Kind: "bool", Description: "Also start agentgateway in front of klaus-gateway."},
	{CLIFlag: "port", MCPKey: "port", Kind: "int", Description: "Override the klaus-gateway listen port."},
	{CLIFlag: "agentgateway-port", MCPKey: "agentgatewayPort", Kind: "int", Description: "Override the agentgateway listen port."},
	{CLIFlag: "klaus-gateway-bin", MCPKey: "klausGatewayBin", Kind: "string", Description: "Path to the klaus-gateway host binary (overrides KLAUS_GATEWAY_BIN)."},
	{CLIFlag: "agentgateway-bin", MCPKey: "agentgatewayBin", Kind: "string", Description: "Path to the agentgateway host binary (overrides KLAUS_AGENTGATEWAY_BIN)."},
	{CLIFlag: "log-level", MCPKey: "logLevel", Kind: "string", Description: "Gateway log level (debug, info, warn, error)."},
}

// StatusFlags lists the inputs accepted by `gateway status` and the
// `klaus_gateway_status` MCP tool.
var StatusFlags = []Flag{
	{CLIFlag: "output", MCPKey: "output", Kind: "string", Description: `Output format: "text" (default) or "json".`},
}
