package server

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// RejectUnknownArguments answers a tool call that carries a top-level
// argument the tool's input schema does not declare with an error naming
// each such key and the declared keys closest to it, before the handler
// runs. A dropped argument otherwise surfaces much later — an agent
// launched without the credentials a misspelled secretEnvVars carried
// (giantswarm/klausctl#318). tool looks a registered tool up by name.
func RejectUnknownArguments(tool func(name string) *mcpserver.ServerTool) mcpserver.ToolHandlerMiddleware {
	return func(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			st := tool(req.Params.Name)
			if st == nil || len(st.Tool.RawInputSchema) > 0 {
				return next(ctx, req)
			}
			if msg := unknownArguments(req.Params.Name, st.Tool.InputSchema.Properties, req.GetArguments()); msg != "" {
				return mcp.NewToolResultError(msg), nil
			}
			return next(ctx, req)
		}
	}
}

// unknownArguments is the error for the keys of args that declared does not
// hold, empty when every key is declared.
func unknownArguments(tool string, declared map[string]any, args map[string]any) string {
	var unknown []string
	for key := range args {
		if _, ok := declared[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	known := make([]string, 0, len(declared))
	for key := range declared {
		known = append(known, key)
	}
	sort.Strings(known)
	parts := make([]string, 0, len(unknown))
	for _, key := range unknown {
		part := fmt.Sprintf("%q", key)
		if near := closest(key, known); len(near) > 0 {
			part += fmt.Sprintf(" (did you mean %s?)", quoteJoin(near, " or "))
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("%s: unknown argument(s) %s; nothing was done. %s takes: %s", tool, strings.Join(parts, ", "), tool, strings.Join(known, ", "))
}

// closest is the declared keys a mistyped key most likely meant: those
// sharing its longest common prefix of at least four characters, else
// those within an edit distance of two.
func closest(key string, known []string) []string {
	lower := strings.ToLower(key)
	best, longest := []string(nil), 3
	for _, k := range known {
		switch n := commonPrefix(lower, strings.ToLower(k)); {
		case n > longest:
			best, longest = []string{k}, n
		case n == longest && best != nil:
			best = append(best, k)
		}
	}
	if best != nil {
		return best
	}
	for _, k := range known {
		if editDistance(lower, strings.ToLower(k)) <= 2 {
			best = append(best, k)
		}
	}
	return best
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// editDistance is the Levenshtein distance of a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = slices.Min([]int{prev[j] + 1, cur[j-1] + 1, prev[j-1] + cost})
		}
		prev = cur
	}
	return prev[len(b)]
}

func quoteJoin(keys []string, sep string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(quoted, sep)
}
