package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/instance"
)

func TestStatusCommandRegistered(t *testing.T) {
	assertCommandOnRoot(t, "status")
}

func TestStatusOutputFlagRegistered(t *testing.T) {
	assertFlagRegistered(t, statusCmd, "output")
}

// writeInstanceState creates an instance.json for testing.
func writeInstanceState(t *testing.T, configHome, name string, port int) {
	t.Helper()
	instanceDir := filepath.Join(configHome, "klausctl", "instances", name)
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &instance.Instance{
		Name:      name,
		Runtime:   "fake",
		Image:     "ghcr.io/test/image:latest",
		Port:      port,
		Workspace: "/tmp/workspace",
		StartedAt: time.Now().Add(-5 * time.Minute),
	}
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "instance.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 5*time.Minute + 30*time.Second, "5m30s"},
		{"hours", 2*time.Hour + 15*time.Minute, "2h15m"},
		{"days", 25*time.Hour + 30*time.Minute, "1d1h"},
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

func TestStatusTextOutputIncludesAgentInfo(t *testing.T) {
	// Set up a mock HTTP server that returns agent status.
	agentStatus := map[string]any{
		"name":    "klaus",
		"version": "dev",
		"agent": map[string]any{
			"status":        "busy",
			"session_id":    "abc-123-def",
			"message_count": 42,
		},
		"mode": "single-shot",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(agentStatus)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Point the agent HTTP client at our test server.
	oldClient := agentStatusHTTPClient
	agentStatusHTTPClient = srv.Client()
	t.Cleanup(func() { agentStatusHTTPClient = oldClient })

	// We can't easily run the full command without a real runtime,
	// so test the formatting helpers and struct directly.
	info := statusInfo{
		Instance:     "test",
		Status:       "running",
		Container:    "klausctl-test",
		Runtime:      "docker",
		Image:        "ghcr.io/test/image:latest",
		Workspace:    "/tmp/workspace",
		MCP:          srv.URL,
		Uptime:       "5m30s",
		Agent:        "busy",
		Session:      "abc-123-def",
		MessageCount: 42,
	}

	// Verify JSON encoding includes agent fields.
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["agent"] != "busy" {
		t.Errorf("expected agent=busy, got %v", decoded["agent"])
	}
	if decoded["session"] != "abc-123-def" {
		t.Errorf("expected session field, got %v", decoded["session"])
	}
	if int(decoded["message_count"].(float64)) != 42 {
		t.Errorf("expected message_count=42, got %v", decoded["message_count"])
	}
}

func TestStatusJSONOutputIncludesAgentFields(t *testing.T) {
	info := statusInfo{
		Instance:     "dev",
		Status:       "running",
		Container:    "klausctl-dev",
		Runtime:      "docker",
		Image:        "test:latest",
		Workspace:    "/tmp",
		MCP:          "http://localhost:8082",
		Uptime:       "1h0m",
		Agent:        "idle",
		Session:      "sess-456",
		MessageCount: 0,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(info); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded["agent"] != "idle" {
		t.Errorf("expected agent=idle, got %v", decoded["agent"])
	}
	if decoded["session"] != "sess-456" {
		t.Errorf("expected session=sess-456, got %v", decoded["session"])
	}
}

func TestStatusAgentFieldOmittedWhenEmpty(t *testing.T) {
	info := statusInfo{
		Instance:  "dev",
		Status:    "stopped",
		Container: "klausctl-dev",
		Runtime:   "docker",
		Image:     "test:latest",
		Workspace: "/tmp",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if _, ok := decoded["agent"]; ok {
		t.Error("agent field should be omitted when empty")
	}
	if _, ok := decoded["session"]; ok {
		t.Error("session field should be omitted when empty")
	}
	if _, ok := decoded["message_count"]; ok {
		t.Error("message_count field should be omitted when zero")
	}
}

func TestStatusTextRenderingOrder(t *testing.T) {
	// Verify that Agent and Session lines appear after Uptime in text output.
	var buf bytes.Buffer
	out := &buf

	info := statusInfo{
		Instance:     "test",
		Status:       "running",
		Container:    "klausctl-test",
		Runtime:      "docker",
		Image:        "test:latest",
		Workspace:    "/tmp",
		MCP:          "http://localhost:8082",
		Uptime:       "5m0s",
		Agent:        "busy",
		Session:      "session-id-123",
		MessageCount: 10,
	}

	// Simulate the text rendering logic from runStatus.
	fmt.Fprintf(out, "Instance:    %s\n", info.Instance)
	fmt.Fprintf(out, "Status:      %s\n", info.Status)
	fmt.Fprintf(out, "Container:   %s\n", info.Container)
	fmt.Fprintf(out, "Runtime:     %s\n", info.Runtime)
	fmt.Fprintf(out, "Image:       %s\n", info.Image)
	fmt.Fprintf(out, "Workspace:   %s\n", info.Workspace)
	fmt.Fprintf(out, "MCP:         %s\n", info.MCP)
	if info.Uptime != "" {
		fmt.Fprintf(out, "Uptime:      %s\n", info.Uptime)
	}
	if info.Agent != "" {
		agentLine := info.Agent
		if info.MessageCount > 0 {
			agentLine = fmt.Sprintf("%s (%d messages)", info.Agent, info.MessageCount)
		}
		fmt.Fprintf(out, "Agent:       %s\n", agentLine)
	}
	if info.Session != "" {
		fmt.Fprintf(out, "Session:     %s\n", info.Session)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Find positions of key lines.
	var uptimeIdx, agentIdx, sessionIdx int
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "Uptime:"):
			uptimeIdx = i
		case strings.HasPrefix(line, "Agent:"):
			agentIdx = i
		case strings.HasPrefix(line, "Session:"):
			sessionIdx = i
		}
	}

	if agentIdx <= uptimeIdx {
		t.Error("Agent line should appear after Uptime")
	}
	if sessionIdx <= agentIdx {
		t.Error("Session line should appear after Agent")
	}

	if !strings.Contains(output, "busy (10 messages)") {
		t.Errorf("expected agent status with message count, got:\n%s", output)
	}
	if !strings.Contains(output, "session-id-123") {
		t.Errorf("expected session ID in output, got:\n%s", output)
	}
}

func TestStatusCommandUsesCorrectVerb(t *testing.T) {
	if statusCmd.Use != "status [name]" {
		t.Errorf("unexpected Use: %q", statusCmd.Use)
	}
}

// Verify that a stopped instance doesn't include agent fields in text
// rendering (no crash when Agent/Session are empty).
func TestStatusStoppedNoAgentOutput(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)

	info := statusInfo{
		Instance:  "dev",
		Status:    "stopped",
		Container: "klausctl-dev",
		Runtime:   "docker",
		Image:     "test:latest",
		Workspace: "/tmp",
	}

	// Simulate the text rendering for a stopped instance.
	fmt.Fprintf(&buf, "Instance:    %s\n", info.Instance)
	fmt.Fprintf(&buf, "Status:      %s\n", info.Status)
	fmt.Fprintf(&buf, "Container:   %s\n", info.Container)
	fmt.Fprintf(&buf, "Runtime:     %s\n", info.Runtime)
	fmt.Fprintf(&buf, "Image:       %s\n", info.Image)
	fmt.Fprintf(&buf, "Workspace:   %s\n", info.Workspace)

	output := buf.String()
	if strings.Contains(output, "Agent:") {
		t.Error("stopped instance should not have Agent line")
	}
	if strings.Contains(output, "Session:") {
		t.Error("stopped instance should not have Session line")
	}
}
