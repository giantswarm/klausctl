package cache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/ocicache"
)

func testServerContext(t *testing.T) *server.ServerContext {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	return &server.ServerContext{Paths: paths}
}

func TestRegisterTools_Registers(t *testing.T) {
	sc := testServerContext(t)
	srv := mcpserver.NewMCPServer("test", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	RegisterTools(srv, sc)
}

// resultText extracts the text payload from a CallToolResult. The mcp-go
// result uses a Content interface; for JSONResult output the first entry
// is always a mcp.TextContent.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content %T", res.Content[0])
	}
	return tc.Text
}

func TestHandleInfo_Disabled(t *testing.T) {
	ocicache.Configure("", true)
	t.Cleanup(ocicache.Reset)

	res, err := handleInfo(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var info ocicache.Info
	if err := json.Unmarshal([]byte(resultText(t, res)), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !info.Disabled {
		t.Error("info.Disabled = false, want true")
	}
}

func TestHandlePrune_All(t *testing.T) {
	dir := t.TempDir()
	ocicache.Configure(dir, false)
	t.Cleanup(ocicache.Reset)

	entry := filepath.Join(dir, "refs", "x.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`{"key":"host/repo:tag"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"all": true}
	res, err := handlePrune(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var pr ocicache.PruneResult
	if err := json.Unmarshal([]byte(resultText(t, res)), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.FilesRemoved != 1 {
		t.Errorf("FilesRemoved = %d, want 1", pr.FilesRemoved)
	}
}

func TestHandleRefresh_RejectsBothScopes(t *testing.T) {
	ocicache.Configure(t.TempDir(), false)
	t.Cleanup(ocicache.Reset)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"registry": "host.example.com",
		"repo":     "host.example.com/repo",
	}
	res, err := handleRefresh(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected error result when both registry and repo provided")
	}
	if !strings.Contains(resultText(t, res), "mutually exclusive") {
		t.Errorf("error text = %q, want 'mutually exclusive'", resultText(t, res))
	}
}
