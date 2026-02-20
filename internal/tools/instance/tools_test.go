package instance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

func testServerContext(t *testing.T) *server.ServerContext {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(paths.InstancesDir); err != nil {
		t.Fatal(err)
	}
	return &server.ServerContext{Paths: paths, MCPClient: mcpclient.New("test")}
}

func TestRegisterTools(t *testing.T) {
	sc := testServerContext(t)
	srv := mcpserver.NewMCPServer("test", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	RegisterTools(srv, sc)
}

func TestHandleListEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := mcp.CallToolRequest{}
	result, err := handleList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertJSONArray(t, result, 0)
}

func TestHandleStatusMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleStatus(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStatusStoppedInstance(t *testing.T) {
	sc := testServerContext(t)

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "stopped-inst")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Image = "example.com/test:v1"
	cfg.Workspace = "/tmp"
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{"name": "stopped-inst"})
	result, err := handleStatus(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var obj map[string]string
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if obj["status"] != "stopped" {
		t.Errorf("expected 'stopped' status, got %q", obj["status"])
	}
	if obj["instance"] != "stopped-inst" {
		t.Errorf("expected instance 'stopped-inst', got %q", obj["instance"])
	}
}

func TestHandleLogsMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleLogs(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleDeleteMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleDelete(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleCreateInvalidName(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "INVALID NAME!"})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleCreateDuplicateInstance(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "existing")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "existing",
		"workspace": workspace,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStopRequiresNameOrAll(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStopNameAndAllMutuallyExclusive(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{
		"name": "test",
		"all":  true,
	})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStopNotRunning(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := extractResultText(t, result)
	var obj map[string]string
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", data)
	}
	if obj["status"] != "not running" {
		t.Errorf("expected 'not running' status, got %q", obj["status"])
	}
}

func TestHandleStopAllEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"all": true})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := extractResultText(t, result)
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", data)
	}
	if obj["status"] != "all stopped" {
		t.Errorf("expected 'all stopped', got %v", obj["status"])
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 150 * time.Second, "2m30s"},
		{"hours", 90 * time.Minute, "1h30m"},
		{"days", 25 * time.Hour, "1d1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestApplyCreateOverrides(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		check   func(t *testing.T, cfg *config.Config)
		wantErr bool
	}{
		{
			name: "envVars sets environment variables",
			args: map[string]any{
				"envVars": map[string]any{
					"GITHUB_TOKEN": "tok-123",
					"MY_VAR":       "hello",
				},
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.EnvVars["GITHUB_TOKEN"] != "tok-123" {
					t.Errorf("expected GITHUB_TOKEN=tok-123, got %q", cfg.EnvVars["GITHUB_TOKEN"])
				}
				if cfg.EnvVars["MY_VAR"] != "hello" {
					t.Errorf("expected MY_VAR=hello, got %q", cfg.EnvVars["MY_VAR"])
				}
			},
		},
		{
			name: "envVars rejects non-string value",
			args: map[string]any{
				"envVars": map[string]any{"BAD": 42},
			},
			wantErr: true,
		},
		{
			name: "envVars rejects non-object",
			args: map[string]any{
				"envVars": "not-an-object",
			},
			wantErr: true,
		},
		{
			name: "envForward appends forwarded vars",
			args: map[string]any{
				"envForward": []any{"SSH_AUTH_SOCK", "HOME"},
			},
			check: func(t *testing.T, cfg *config.Config) {
				if len(cfg.EnvForward) != 2 {
					t.Fatalf("expected 2 envForward entries, got %d", len(cfg.EnvForward))
				}
				if cfg.EnvForward[0] != "HOME" || cfg.EnvForward[1] != "SSH_AUTH_SOCK" {
					t.Errorf("unexpected envForward: %v", cfg.EnvForward)
				}
			},
		},
		{
			name: "envForward deduplicates entries",
			args: map[string]any{
				"envForward": []any{"HOME", "SSH_AUTH_SOCK", "HOME"},
			},
			check: func(t *testing.T, cfg *config.Config) {
				want := []string{"HOME", "SSH_AUTH_SOCK"}
				if len(cfg.EnvForward) != len(want) {
					t.Fatalf("expected %d envForward entries, got %d: %v", len(want), len(cfg.EnvForward), cfg.EnvForward)
				}
				for i, v := range want {
					if cfg.EnvForward[i] != v {
						t.Errorf("envForward[%d] = %q, want %q", i, cfg.EnvForward[i], v)
					}
				}
			},
		},
		{
			name: "mcpServers sets MCP server config",
			args: map[string]any{
				"mcpServers": map[string]any{
					"github": map[string]any{
						"type": "http",
						"url":  "https://api.example.com/mcp/",
					},
				},
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.McpServers == nil {
					t.Fatal("mcpServers is nil")
				}
				gh, ok := cfg.McpServers["github"]
				if !ok {
					t.Fatal("expected 'github' key in mcpServers")
				}
				m := gh.(map[string]any)
				if m["type"] != "http" {
					t.Errorf("expected type=http, got %v", m["type"])
				}
			},
		},
		{
			name: "mcpServers rejects non-object",
			args: map[string]any{
				"mcpServers": "bad",
			},
			wantErr: true,
		},
		{
			name: "maxBudgetUsd sets budget",
			args: map[string]any{
				"maxBudgetUsd": float64(10),
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.Claude.MaxBudgetUSD != 10 {
					t.Errorf("expected maxBudgetUsd=10, got %f", cfg.Claude.MaxBudgetUSD)
				}
			},
		},
		{
			name: "permissionMode sets mode",
			args: map[string]any{
				"permissionMode": "dontAsk",
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.Claude.PermissionMode != "dontAsk" {
					t.Errorf("expected permissionMode=dontAsk, got %q", cfg.Claude.PermissionMode)
				}
			},
		},
		{
			name: "invalid permissionMode rejected by validation",
			args: map[string]any{
				"permissionMode": "invalid",
			},
			wantErr: true,
		},
		{
			name: "model sets Claude model",
			args: map[string]any{
				"model": "opus",
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.Claude.Model != "opus" {
					t.Errorf("expected model=opus, got %q", cfg.Claude.Model)
				}
			},
		},
		{
			name: "systemPrompt sets prompt",
			args: map[string]any{
				"systemPrompt": "You are a helpful assistant.",
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.Claude.SystemPrompt != "You are a helpful assistant." {
					t.Errorf("expected systemPrompt override, got %q", cfg.Claude.SystemPrompt)
				}
			},
		},
		{
			name: "all overrides combined",
			args: map[string]any{
				"envVars":        map[string]any{"KEY": "val"},
				"envForward":     []any{"HOME"},
				"maxBudgetUsd":   float64(5),
				"permissionMode": "acceptEdits",
				"model":          "sonnet",
				"systemPrompt":   "Be concise.",
			},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.EnvVars["KEY"] != "val" {
					t.Error("envVars not applied")
				}
				if len(cfg.EnvForward) != 1 || cfg.EnvForward[0] != "HOME" {
					t.Error("envForward not applied")
				}
				if cfg.Claude.MaxBudgetUSD != 5 {
					t.Error("maxBudgetUsd not applied")
				}
				if cfg.Claude.PermissionMode != "acceptEdits" {
					t.Error("permissionMode not applied")
				}
				if cfg.Claude.Model != "sonnet" {
					t.Error("model not applied")
				}
				if cfg.Claude.SystemPrompt != "Be concise." {
					t.Error("systemPrompt not applied")
				}
			},
		},
		{
			name: "no overrides leaves defaults untouched",
			args: map[string]any{},
			check: func(t *testing.T, cfg *config.Config) {
				if cfg.Claude.PermissionMode != "bypassPermissions" {
					t.Errorf("default permissionMode changed to %q", cfg.Claude.PermissionMode)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Workspace = "/tmp"

			req := callToolRequest(tt.args)
			err := applyCreateOverrides(req, cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestShortToolchainName(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"gsoci.azurecr.io/giantswarm/klaus-go:1.0.0", "go"},
		{"gsoci.azurecr.io/giantswarm/some-image:latest", "some-image"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			cfg := &config.Config{Image: tt.image}
			got := shortToolchainName(cfg)
			if got != tt.want {
				t.Errorf("shortToolchainName(image=%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

// --- helpers ---

func callToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func assertIsError(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.IsError {
		t.Errorf("expected error result, got success: %+v", result)
	}
}

func assertJSONArray(t *testing.T, result *mcp.CallToolResult, expectedLen int) {
	t.Helper()
	data := extractResultText(t, result)
	var arr []any
	if err := json.Unmarshal([]byte(data), &arr); err != nil {
		t.Fatalf("expected JSON array, got: %s", data)
	}
	if len(arr) != expectedLen {
		t.Errorf("expected %d elements, got %d", expectedLen, len(arr))
	}
}

func extractResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	content := result.Content[0]
	textContent, ok := content.(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", content)
	}
	return textContent.Text
}
