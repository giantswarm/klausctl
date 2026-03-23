package instance

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestWaitForHTTPReadyMCPCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}

	err := waitForHTTPReadyMCP(ctx, httpClient, "http://localhost:0")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWaitForHTTPReadyMCPTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}

	err := waitForHTTPReadyMCP(ctx, httpClient, "http://localhost:0")
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
