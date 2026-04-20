package gatewaybridge

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	healthPollTimeout = 15 * time.Second
	healthPollDelay   = 500 * time.Millisecond
)

// waitHealthy polls http://localhost:<port>/healthz until it returns a non-5xx
// response or the timeout is reached.
func waitHealthy(ctx context.Context, port int) error {
	deadline := time.Now().Add(healthPollTimeout)
	url := fmt.Sprintf("http://localhost:%d/healthz", port)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}

		time.Sleep(healthPollDelay)
	}

	return fmt.Errorf("timed out waiting for /healthz on port %d", port)
}

// healthyNow reports whether /healthz responds non-5xx within a short timeout.
func healthyNow(port int) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/healthz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}
