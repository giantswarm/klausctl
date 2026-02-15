package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
image: ghcr.io/giantswarm/klaus:v1.0.0
workspace: /tmp/test-workspace
port: 9090
claude:
  model: sonnet
  permissionMode: default
  effort: high
  maxTurns: 10
  maxBudgetUsd: 5.0
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Image != "ghcr.io/giantswarm/klaus:v1.0.0" {
		t.Errorf("Image = %q, want %q", cfg.Image, "ghcr.io/giantswarm/klaus:v1.0.0")
	}
	if cfg.Workspace != "/tmp/test-workspace" {
		t.Errorf("Workspace = %q, want %q", cfg.Workspace, "/tmp/test-workspace")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.Claude.Model != "sonnet" {
		t.Errorf("Claude.Model = %q, want %q", cfg.Claude.Model, "sonnet")
	}
	if cfg.Claude.PermissionMode != "default" {
		t.Errorf("Claude.PermissionMode = %q, want %q", cfg.Claude.PermissionMode, "default")
	}
	if cfg.Claude.Effort != "high" {
		t.Errorf("Claude.Effort = %q, want %q", cfg.Claude.Effort, "high")
	}
	if cfg.Claude.MaxTurns != 10 {
		t.Errorf("Claude.MaxTurns = %d, want %d", cfg.Claude.MaxTurns, 10)
	}
	if cfg.Claude.MaxBudgetUSD != 5.0 {
		t.Errorf("Claude.MaxBudgetUSD = %f, want %f", cfg.Claude.MaxBudgetUSD, 5.0)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `workspace: /tmp/test`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Image != "gsoci.azurecr.io/giantswarm/klaus:latest" {
		t.Errorf("default Image = %q, want %q", cfg.Image, "gsoci.azurecr.io/giantswarm/klaus:latest")
	}
	if cfg.Port != 8080 {
		t.Errorf("default Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.Claude.PermissionMode != "bypassPermissions" {
		t.Errorf("default PermissionMode = %q, want %q", cfg.Claude.PermissionMode, "bypassPermissions")
	}
	if cfg.Claude.NoSessionPersistence == nil || !*cfg.Claude.NoSessionPersistence {
		t.Error("default NoSessionPersistence should be true")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("Load() should return error for missing file")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing workspace",
			cfg:     Config{Port: 8080},
			wantErr: true,
			errMsg:  "workspace is required",
		},
		{
			name:    "invalid port zero",
			cfg:     Config{Workspace: "/tmp", Port: 0},
			wantErr: true,
			errMsg:  "port must be between",
		},
		{
			name:    "invalid port too high",
			cfg:     Config{Workspace: "/tmp", Port: 70000},
			wantErr: true,
			errMsg:  "port must be between",
		},
		{
			name:    "invalid runtime",
			cfg:     Config{Workspace: "/tmp", Port: 8080, Runtime: "containerd"},
			wantErr: true,
			errMsg:  "runtime must be",
		},
		{
			name: "invalid permission mode",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Claude: ClaudeConfig{PermissionMode: "invalid"},
			},
			wantErr: true,
			errMsg:  "invalid permission mode",
		},
		{
			name: "invalid effort",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Claude: ClaudeConfig{Effort: "extreme"},
			},
			wantErr: true,
			errMsg:  "invalid effort level",
		},
		{
			name: "negative max turns",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Claude: ClaudeConfig{MaxTurns: -1},
			},
			wantErr: true,
			errMsg:  "maxTurns must be >= 0",
		},
		{
			name: "negative budget",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Claude: ClaudeConfig{MaxBudgetUSD: -1.0},
			},
			wantErr: true,
			errMsg:  "maxBudgetUsd must be >= 0",
		},
		{
			name: "plugin missing tag and digest",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Plugins: []Plugin{{Repository: "example.com/plugin"}},
			},
			wantErr: true,
			errMsg:  "requires either tag or digest",
		},
		{
			name: "plugin missing repository",
			cfg: Config{
				Workspace: "/tmp", Port: 8080,
				Plugins: []Plugin{{Tag: "v1.0.0"}},
			},
			wantErr: true,
			errMsg:  "plugin repository is required",
		},
		{
			name: "valid minimal config",
			cfg: Config{
				Workspace: "/tmp",
				Port:      8080,
			},
			wantErr: false,
		},
		{
			name: "valid full config",
			cfg: Config{
				Workspace: "/tmp",
				Port:      8080,
				Runtime:   "docker",
				Claude: ClaudeConfig{
					PermissionMode: "bypassPermissions",
					Effort:         "medium",
					MaxTurns:       5,
					MaxBudgetUSD:   10.0,
				},
				Plugins: []Plugin{
					{Repository: "example.com/plugin", Tag: "v1.0.0"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() should return error")
				}
				if tt.errMsg != "" {
					if got := err.Error(); !strings.Contains(got, tt.errMsg) {
						t.Errorf("error = %q, want substring %q", got, tt.errMsg)
					}
				}
			} else if err != nil {
				t.Fatalf("Validate() returned unexpected error: %v", err)
			}
		})
	}
}

func TestMarshal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workspace = "/tmp/test"

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Marshal() returned empty data")
	}
}
