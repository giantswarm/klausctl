package instance

import (
	"context"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

func TestWaitForMCPReadyMCPCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sc := &server.ServerContext{
		MCPClient: mcpclient.New("test"),
	}
	defer sc.MCPClient.Close()

	err := waitForMCPReadyMCP(ctx, "test-instance", "http://localhost:0/mcp", sc)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForMCPReadyMCPTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	sc := &server.ServerContext{
		MCPClient: mcpclient.New("test"),
	}
	defer sc.MCPClient.Close()

	err := waitForMCPReadyMCP(ctx, "test-instance", "http://localhost:0/mcp", sc)
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
