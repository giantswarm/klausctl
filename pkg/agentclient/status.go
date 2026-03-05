// Package agentclient provides a lightweight HTTP client for querying
// the klaus agent's /status endpoint. Unlike the MCP client, this does
// not require session initialization — it's a simple GET request.
package agentclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxStatusResponseBytes limits the size of the agent status response
// to prevent memory exhaustion from a misbehaving agent.
const maxStatusResponseBytes = 1 << 20 // 1 MiB

// AgentInfo holds the agent-level fields returned by the /status endpoint.
type AgentInfo struct {
	Status       string `json:"status"`
	SessionID    string `json:"session_id,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
}

// StatusResponse represents the JSON body returned by GET /status.
type StatusResponse struct {
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Agent   AgentInfo `json:"agent"`
	Mode    string    `json:"mode,omitempty"`
}

// FetchStatus queries the agent's HTTP /status endpoint and returns the
// parsed response. Returns an error if the agent is unreachable, returns
// a non-200 status, or returns invalid JSON.
func FetchStatus(ctx context.Context, client *http.Client, baseURL string) (*StatusResponse, error) {
	url := baseURL + "/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying agent status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent returned status %d", resp.StatusCode)
	}

	var status StatusResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxStatusResponseBytes)).Decode(&status); err != nil {
		return nil, fmt.Errorf("decoding agent status: %w", err)
	}

	return &status, nil
}
