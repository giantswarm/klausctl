package instance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/agentclient"
)

func TestWaitForReadyMCPCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}

	err := agentclient.WaitForReady(ctx, httpClient, "http://localhost:0")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForReadyMCPTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}

	err := agentclient.WaitForReady(ctx, httpClient, "http://localhost:0")
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
