// Package remotesurface declares the `--remote` and `--session` flags that
// are shared by `klausctl run`, `klausctl prompt`, and `klausctl messages`
// plus the corresponding MCP tools (klaus_run, klaus_prompt, klaus_messages).
//
// Both the Cobra commands and the MCP tool registrations consume this
// package so the two surfaces cannot drift. A parity test in
// cmd/remote_parity_test.go asserts that every CLI flag has an MCP
// equivalent and vice versa, across all three subcommands/tools.
package remotesurface

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

// Flags lists the remote-targeting inputs accepted by `run`, `prompt`, and
// `messages` (CLI) and `klaus_run`, `klaus_prompt`, `klaus_messages` (MCP).
var Flags = []Flag{
	{
		CLIFlag:     "remote",
		MCPKey:      "remote",
		Kind:        "string",
		Description: "Remote klaus-gateway base URL; when set, the target is the gateway and no local container is inspected or spawned.",
	},
	{
		CLIFlag:     "session",
		MCPKey:      "session",
		Kind:        "string",
		Description: "Session (thread) name forwarded as X-Klaus-Thread-ID (default: stable hash of the current working directory).",
	},
}

// CLIFlagByKey returns the Flag entry for a given CLIFlag name, panicking
// on an unknown key. Use this in Cobra bindings so typos fail at init time.
func CLIFlagByKey(key string) Flag {
	for _, f := range Flags {
		if f.CLIFlag == key {
			return f
		}
	}
	panic("unknown remote flag: " + key)
}
