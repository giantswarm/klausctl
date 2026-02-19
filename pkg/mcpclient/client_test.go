package mcpclient

import (
	"context"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := New("test")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.sessions == nil {
		t.Fatal("expected non-nil sessions map")
	}
	if c.version != "test" {
		t.Errorf("expected version %q, got %q", "test", c.version)
	}
}

func TestSessionIDMissing(t *testing.T) {
	c := New("test")
	if id := c.SessionID("nonexistent"); id != "" {
		t.Errorf("expected empty session ID, got %q", id)
	}
}

func TestClose(t *testing.T) {
	c := New("test")
	c.Close()
	if len(c.sessions) != 0 {
		t.Errorf("expected empty sessions after close")
	}
}

func TestPromptUnreachable(t *testing.T) {
	c := New("test")
	defer c.Close()

	ctx := context.Background()
	_, err := c.Prompt(ctx, "test", "http://127.0.0.1:1/mcp", "hello")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestStatusUnreachable(t *testing.T) {
	c := New("test")
	defer c.Close()

	ctx := context.Background()
	_, err := c.Status(ctx, "test", "http://127.0.0.1:1/mcp")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestResultUnreachable(t *testing.T) {
	c := New("test")
	defer c.Close()

	ctx := context.Background()
	_, err := c.Result(ctx, "test", "http://127.0.0.1:1/mcp")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestInvalidateSession(t *testing.T) {
	c := New("test")
	defer c.Close()

	c.invalidateSession("nonexistent")
	if len(c.sessions) != 0 {
		t.Errorf("expected empty sessions")
	}
}
